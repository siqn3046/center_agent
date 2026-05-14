package mydispatcher

//go:generate go run github.com/xtls/xray-core/common/errors/errorgen

import (
	"context"
	perrors "errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/dns"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/features/routing"
	routingSession "github.com/xtls/xray-core/features/routing/session"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/transport"
	"github.com/xtls/xray-core/transport/pipe"

	"github.com/XrayR-project/XrayR/common/limiter"
	"github.com/XrayR-project/XrayR/common/rule"
)

var errSniffingTimeout = newError("timeout on sniffing")

// ensureTimeoutReader returns r when it already supports timed reads (e.g. *pipe.Reader).
// Otherwise wraps r with queuedTimeoutBufReader so timed sniff reads never drop bytes that
// complete after the deadline (buf.TimeoutWrapperReader can race with VisionReader).
func ensureTimeoutReader(r buf.Reader) buf.TimeoutReader {
	if tr, ok := r.(buf.TimeoutReader); ok {
		return tr
	}
	return &queuedTimeoutBufReader{inner: r}
}

// wrapDispatchLinkReader mirrors upstream dispatcher.WrapLink: wrap Reader for policy/stats,
// using queuedTimeoutBufReader so DispatchLink sniff + TLS replay stays lossless with Vision.
func wrapDispatchLinkReader(ctx context.Context, pm policy.Manager, sm stats.Manager, link *transport.Link) *transport.Link {
	if link == nil {
		return nil
	}
	inner := link.Reader
	if tw, ok := inner.(*buf.TimeoutWrapperReader); ok {
		inner = tw.Reader
	}
	if _, ok := inner.(*queuedTimeoutBufReader); !ok {
		link.Reader = &queuedTimeoutBufReader{inner: inner}
	} else {
		link.Reader = inner
	}
	qr := link.Reader.(*queuedTimeoutBufReader)
	sessionInbound := session.InboundFromContext(ctx)
	var user *protocol.MemoryUser
	if sessionInbound != nil {
		user = sessionInbound.User
	}
	if user != nil && len(user.Email) > 0 {
		p := pm.ForLevel(user.Level)
		if p.Stats.UserUplink {
			name := "user>>>" + user.Email + ">>>traffic>>>uplink"
			if c, _ := stats.GetOrRegisterCounter(sm, name); c != nil {
				qr.Counter = c
			}
		}
		if p.Stats.UserDownlink {
			name := "user>>>" + user.Email + ">>>traffic>>>downlink"
			if c, _ := stats.GetOrRegisterCounter(sm, name); c != nil {
				link.Writer = &SizeStatWriter{
					Counter: c,
					Writer:  link.Writer,
				}
			}
		}
		if p.Stats.UserOnline {
			name := "user>>>" + user.Email + ">>>online"
			if om, _ := stats.GetOrRegisterOnlineMap(sm, name); om != nil {
				userIP := sessionInbound.Source.Address.String()
				om.AddIP(userIP)
				context.AfterFunc(ctx, func() { om.RemoveIP(userIP) })
			}
		}
	}
	return link
}

type cachedReader struct {
	sync.Mutex
	reader buf.TimeoutReader
	cache  buf.MultiBuffer
	logCtx context.Context
}

func (r *cachedReader) Cache(b *buf.Buffer, deadline time.Duration) error {
	mb, err := r.reader.ReadMultiBufferTimeout(deadline)
	if err != nil {
		if r.logCtx != nil {
			errors.LogDebug(r.logCtx, "vision_sniff_error_but_replay_continue", err)
		}
		return err
	}
	r.Lock()
	if !mb.IsEmpty() {
		r.cache, _ = buf.MergeMulti(r.cache, mb)
		if r.logCtx != nil {
			errors.LogDebug(r.logCtx, "vision_sniff_bytes_read", mb.Len())
		}
	}
	b.Clear()
	rawBytes := b.Extend(min(r.cache.Len(), b.Cap()))
	n := r.cache.Copy(rawBytes)
	b.Resize(0, int32(n))
	r.Unlock()
	return nil
}

func (r *cachedReader) readInternal() buf.MultiBuffer {
	r.Lock()
	defer r.Unlock()

	if r.cache != nil && !r.cache.IsEmpty() {
		mb := r.cache
		r.cache = nil
		if r.logCtx != nil {
			errors.LogDebug(r.logCtx, "vision_sniff_replay_bytes", mb.Len())
		}
		return mb
	}

	return nil
}

func (r *cachedReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb := r.readInternal()
	if mb != nil {
		return mb, nil
	}

	return r.reader.ReadMultiBuffer()
}

func (r *cachedReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	mb := r.readInternal()
	if mb != nil {
		return mb, nil
	}

	return r.reader.ReadMultiBufferTimeout(timeout)
}

func (r *cachedReader) Interrupt() {
	r.Lock()
	if r.cache != nil {
		r.cache = buf.ReleaseMulti(r.cache)
	}
	r.Unlock()
	if p, ok := r.reader.(*pipe.Reader); ok {
		p.Interrupt()
		return
	}
	if q, ok := r.reader.(*queuedTimeoutBufReader); ok {
		q.Interrupt()
	}
}

// DefaultDispatcher is a default implementation of Dispatcher.
type DefaultDispatcher struct {
	ohm         outbound.Manager
	router      routing.Router
	policy      policy.Manager
	stats       stats.Manager
	dns         dns.Client
	fdns        dns.FakeDNSEngine
	Limiter     *limiter.Limiter
	RuleManager *rule.Manager
}

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		d := new(DefaultDispatcher)
		if err := core.RequireFeatures(ctx, func(om outbound.Manager, router routing.Router, pm policy.Manager, sm stats.Manager, dc dns.Client) error {
			core.OptionalFeatures(ctx, func(fdns dns.FakeDNSEngine) {
				d.fdns = fdns
			})
			return d.Init(config.(*Config), om, router, pm, sm, dc)
		}); err != nil {
			return nil, err
		}
		return d, nil
	}))
}

// Init initializes DefaultDispatcher.
func (d *DefaultDispatcher) Init(config *Config, om outbound.Manager, router routing.Router, pm policy.Manager, sm stats.Manager, dns dns.Client) error {
	d.ohm = om
	d.router = router
	d.policy = pm
	d.stats = sm
	d.Limiter = limiter.New()
	d.RuleManager = rule.New()
	d.dns = dns
	return nil
}

// Type implements common.HasType.
func (*DefaultDispatcher) Type() interface{} {
	return routing.DispatcherType()
}

// Start implements common.Runnable.
func (*DefaultDispatcher) Start() error {
	return nil
}

// Close implements common.Closable.
func (*DefaultDispatcher) Close() error {
	return nil
}

func (d *DefaultDispatcher) getLink(ctx context.Context, network net.Network, sniffing session.SniffingRequest) (*transport.Link, *transport.Link, error) {
	opt := pipe.OptionsFromContext(ctx)
	uplinkReader, uplinkWriter := pipe.New(opt...)
	downlinkReader, downlinkWriter := pipe.New(opt...)

	inboundLink := &transport.Link{
		Reader: downlinkReader,
		Writer: uplinkWriter,
	}

	outboundLink := &transport.Link{
		Reader: uplinkReader,
		Writer: downlinkWriter,
	}

	sessionInbound := session.InboundFromContext(ctx)
	var user *protocol.MemoryUser
	if sessionInbound != nil {
		user = sessionInbound.User
	}

	if user != nil && len(user.Email) > 0 {
		// Speed Limit and Device Limit
		bucket, ok, reject := d.Limiter.GetUserBucket(sessionInbound.Tag, user.Email, sessionInbound.Source.Address.IP().String())
		if reject {
			errors.LogWarning(ctx, "Devices reach the limit: ", user.Email)
			common.Close(outboundLink.Writer)
			common.Close(inboundLink.Writer)
			common.Interrupt(outboundLink.Reader)
			common.Interrupt(inboundLink.Reader)
			return nil, nil, newError("Devices reach the limit: ", user.Email)
		}
		if ok {
			inboundLink.Writer = d.Limiter.RateWriter(inboundLink.Writer, bucket)
			outboundLink.Writer = d.Limiter.RateWriter(outboundLink.Writer, bucket)
		}

		p := d.policy.ForLevel(user.Level)
		if p.Stats.UserUplink {
			name := "user>>>" + user.Email + ">>>traffic>>>uplink"
			if c, _ := stats.GetOrRegisterCounter(d.stats, name); c != nil {
				inboundLink.Writer = &SizeStatWriter{
					Counter: c,
					Writer:  inboundLink.Writer,
				}
			}
		}
		if p.Stats.UserDownlink {
			name := "user>>>" + user.Email + ">>>traffic>>>downlink"
			if c, _ := stats.GetOrRegisterCounter(d.stats, name); c != nil {
				outboundLink.Writer = &SizeStatWriter{
					Counter: c,
					Writer:  outboundLink.Writer,
				}
			}
		}
	}

	return inboundLink, outboundLink, nil
}

func (d *DefaultDispatcher) shouldOverride(ctx context.Context, result SniffResult, request session.SniffingRequest, destination net.Destination) bool {
	domain := result.Domain()
	if domain == "" {
		return false
	}
	if request.ExcludeForDomain != nil && request.ExcludeForDomain.MatchAny(strings.ToLower(domain)) {
		return false
	}
	if request.ExcludeForIP != nil && destination.Address.Family().IsIP() && request.ExcludeForIP.Match(destination.Address.IP()) {
		return false
	}
	protocolString := result.Protocol()
	if resComp, ok := result.(SnifferResultComposite); ok {
		protocolString = resComp.ProtocolForDomainResult()
	}
	for _, p := range request.OverrideDestinationForProtocol {
		if strings.HasPrefix(protocolString, p) || strings.HasPrefix(p, protocolString) {
			return true
		}
		if fkr0, ok := d.fdns.(dns.FakeDNSEngineRev0); ok && protocolString != "bittorrent" && p == "fakedns" &&
			fkr0.IsIPInIPPool(destination.Address) {
			errors.LogInfo(ctx, "Using sniffer ", protocolString, " since the fake DNS missed")
			return true
		}
		if resultSubset, ok := result.(SnifferIsProtoSubsetOf); ok {
			if resultSubset.IsProtoSubsetOf(p) {
				return true
			}
		}
	}

	return false
}

// Dispatch implements routing.Dispatcher.
func (d *DefaultDispatcher) Dispatch(ctx context.Context, destination net.Destination) (*transport.Link, error) {
	mainCtx := ctx
	if !destination.IsValid() {
		panic("Dispatcher: Invalid destination.")
	}
	outbounds := session.OutboundsFromContext(ctx)
	if len(outbounds) == 0 {
		outbounds = []*session.Outbound{{}}
		ctx = session.ContextWithOutbounds(ctx, outbounds)
	}
	ob := outbounds[len(outbounds)-1]
	ob.OriginalTarget = destination
	ob.Target = destination
	content := session.ContentFromContext(ctx)
	if content == nil {
		content = new(session.Content)
		ctx = session.ContextWithContent(ctx, content)
	}

	sniffingRequest := content.SniffingRequest
	inbound, outbound, err := d.getLink(ctx, destination.Network, sniffingRequest)
	if err != nil {
		return nil, err
	}
	if !sniffingRequest.Enabled || visionSniffReplayDisabled() {
		if sniffingRequest.Enabled && visionSniffReplayDisabled() {
			errors.LogDebug(ctx, "vision_sniff_replay_skipped_by_switch", true)
		}
		go d.routedDispatch(mainCtx, outbound, destination)
	} else {
		go func() {
			errors.LogDebug(ctx, "vision_sniff_start", "dispatch", true, "start_epoch_ms", time.Now().UnixMilli(), "routeOnly", sniffingRequest.RouteOnly, "metadataOnly", sniffingRequest.MetadataOnly, "overrideProtoCount", len(sniffingRequest.OverrideDestinationForProtocol))
			cReader := &cachedReader{
				reader: ensureTimeoutReader(outbound.Reader),
				logCtx: ctx,
			}
			outbound.Reader = cReader
			sniffCtx, sniffCancel := context.WithTimeout(context.WithoutCancel(mainCtx), 30*time.Second)
			defer sniffCancel()
			result, err := sniffer(sniffCtx, cReader, sniffingRequest.MetadataOnly, destination.Network)
			if err != nil {
				errors.LogDebug(mainCtx, "vision_sniff_return_err", err)
				if mainCtx.Err() != nil && (perrors.Is(err, context.Canceled) || perrors.Is(err, context.DeadlineExceeded)) {
					errors.LogDebug(mainCtx, "vision_sniff_ctx_dead_abort_routed_dispatch", err)
					common.Close(outbound.Writer)
					common.Interrupt(outbound.Reader)
					return
				}
				if sniffCtx.Err() != nil && mainCtx.Err() == nil {
					errors.LogDebug(mainCtx, "vision_sniff_child_ctx_done", sniffCtx.Err())
				}
			}
			if err == nil {
				content.Protocol = result.Protocol()
			}
			if err == nil && d.shouldOverride(ctx, result, sniffingRequest, destination) {
				errors.LogDebug(ctx, "vision_sniff_dest_override", true, "vision_sniff_route_only", sniffingRequest.RouteOnly)
				domain := result.Domain()
				errors.LogInfo(ctx, "sniffed domain: ", domain)
				destination.Address = net.ParseAddress(domain)
				protocol := result.Protocol()
				if resComp, ok := result.(SnifferResultComposite); ok {
					protocol = resComp.ProtocolForDomainResult()
				}
				isFakeIP := false
				if fkr0, ok := d.fdns.(dns.FakeDNSEngineRev0); ok && fkr0.IsIPInIPPool(ob.Target.Address) {
					isFakeIP = true
				}
				if sniffingRequest.RouteOnly && protocol != "fakedns" && protocol != "fakedns+others" && !isFakeIP {
					ob.RouteTarget = destination
				} else {
					ob.Target = destination
				}
			} else if err == nil {
				errors.LogDebug(ctx, "vision_sniff_dest_override", false, "vision_sniff_route_only", sniffingRequest.RouteOnly)
			}
			errors.LogDebug(ctx, "vision_sniff_handler_reader_ready")
			d.routedDispatch(mainCtx, outbound, destination)
		}()
	}
	return inbound, nil
}

// DispatchLink implements routing.Dispatcher.
func (d *DefaultDispatcher) DispatchLink(ctx context.Context, destination net.Destination, outbound *transport.Link) error {
	mainCtx := ctx
	if !destination.IsValid() {
		return newError("Dispatcher: Invalid destination.")
	}
	outbounds := session.OutboundsFromContext(ctx)
	if len(outbounds) == 0 {
		outbounds = []*session.Outbound{{}}
		ctx = session.ContextWithOutbounds(ctx, outbounds)
	}
	ob := outbounds[len(outbounds)-1]
	ob.OriginalTarget = destination
	ob.Target = destination
	content := session.ContentFromContext(ctx)
	if content == nil {
		content = new(session.Content)
		ctx = session.ContextWithContent(ctx, content)
	}
	outbound = wrapDispatchLinkReader(ctx, d.policy, d.stats, outbound)
	sniffingRequest := content.SniffingRequest
	if !sniffingRequest.Enabled || visionSniffReplayDisabled() {
		if sniffingRequest.Enabled && visionSniffReplayDisabled() {
			errors.LogDebug(ctx, "vision_sniff_replay_skipped_by_switch", true)
		}
		// 必须与上游 xray-core app/dispatcher 一致：DispatchLink 内同步 routedDispatch。
		// proxyman/inbound/worker 在 Process 返回后会 cancel(ctx)；若此处 go 异步，handler.Dispatch 拿到已取消的 ctx，freedom DNS/dial 会失败。
		d.routedDispatch(mainCtx, outbound, destination)
	} else {
		errors.LogDebug(ctx, "vision_sniff_start", "dispatchLink", true, "start_epoch_ms", time.Now().UnixMilli(), "routeOnly", sniffingRequest.RouteOnly, "metadataOnly", sniffingRequest.MetadataOnly, "overrideProtoCount", len(sniffingRequest.OverrideDestinationForProtocol))
		tr, ok := outbound.Reader.(buf.TimeoutReader)
		if !ok {
			tr = ensureTimeoutReader(outbound.Reader)
		}
		cReader := &cachedReader{
			reader: tr,
			logCtx: ctx,
		}
		outbound.Reader = cReader
		sniffCtx, sniffCancel := context.WithTimeout(context.WithoutCancel(mainCtx), 30*time.Second)
		defer sniffCancel()
		result, err := sniffer(sniffCtx, cReader, sniffingRequest.MetadataOnly, destination.Network)
		if err != nil {
			errors.LogDebug(mainCtx, "vision_sniff_return_err", err)
			if mainCtx.Err() != nil && (perrors.Is(err, context.Canceled) || perrors.Is(err, context.DeadlineExceeded)) {
				errors.LogDebug(mainCtx, "vision_sniff_ctx_dead_abort_routed_dispatch", err)
				common.Close(outbound.Writer)
				common.Interrupt(outbound.Reader)
				return nil
			}
			if sniffCtx.Err() != nil && mainCtx.Err() == nil {
				errors.LogDebug(mainCtx, "vision_sniff_child_ctx_done", sniffCtx.Err())
			}
		}
		if err == nil {
			content.Protocol = result.Protocol()
		}
		if err == nil && d.shouldOverride(ctx, result, sniffingRequest, destination) {
			errors.LogDebug(ctx, "vision_sniff_dest_override", true, "vision_sniff_route_only", sniffingRequest.RouteOnly)
			domain := result.Domain()
			errors.LogInfo(ctx, "sniffed domain: ", domain)
			destination.Address = net.ParseAddress(domain)
			protocol := result.Protocol()
			if resComp, ok := result.(SnifferResultComposite); ok {
				protocol = resComp.ProtocolForDomainResult()
			}
			isFakeIP := false
			if fkr0, ok := d.fdns.(dns.FakeDNSEngineRev0); ok && fkr0.IsIPInIPPool(ob.Target.Address) {
				isFakeIP = true
			}
			if sniffingRequest.RouteOnly && protocol != "fakedns" && protocol != "fakedns+others" && !isFakeIP {
				ob.RouteTarget = destination
			} else {
				ob.Target = destination
			}
		} else if err == nil {
			errors.LogDebug(ctx, "vision_sniff_dest_override", false, "vision_sniff_route_only", sniffingRequest.RouteOnly)
		}
		errors.LogDebug(ctx, "vision_sniff_handler_reader_ready")
		d.routedDispatch(mainCtx, outbound, destination)
	}

	return nil
}

func sniffer(ctx context.Context, cReader *cachedReader, metadataOnly bool, network net.Network) (SniffResult, error) {
	payload := buf.NewWithSize(32767)
	defer payload.Release()

	sniffer := NewSniffer(ctx)

	metaresult, metadataErr := sniffer.SniffMetadata(ctx)
	if metadataErr != nil {
		errors.LogDebug(ctx, "vision_sniff_metadata_err", metadataErr)
	}

	if metadataOnly {
		return metaresult, metadataErr
	}

	contentResult, contentErr := func() (SniffResult, error) {
		cacheDeadline := 200 * time.Millisecond
		totalAttempt := 0
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				cachingStartingTimeStamp := time.Now()
				err := cReader.Cache(payload, cacheDeadline)
				if err != nil {
					return nil, err
				}
				cachingTimeElapsed := time.Since(cachingStartingTimeStamp)
				cacheDeadline -= cachingTimeElapsed

				if !payload.IsEmpty() {
					result, err := sniffer.Sniff(ctx, payload.Bytes(), network)
					switch err {
					case common.ErrNoClue:
						totalAttempt++
					case protocol.ErrProtoNeedMoreData:
					default:
						return result, err
					}
				} else {
					totalAttempt++
				}
				if totalAttempt >= 2 || cacheDeadline <= 0 {
					return nil, errSniffingTimeout
				}
			}
		}
	}()
	if contentErr != nil && metadataErr == nil {
		if perrors.Is(contentErr, context.Canceled) || perrors.Is(contentErr, context.DeadlineExceeded) {
			errors.LogDebug(ctx, "vision_sniff_ctx_err_not_swallowed", contentErr)
			return metaresult, contentErr
		}
		return metaresult, nil
	}
	if contentErr != nil {
		errors.LogDebug(ctx, "vision_sniff_content_err_final", contentErr)
	}
	if contentErr == nil && metadataErr == nil {
		if contentResult != nil {
			errors.LogDebug(ctx, "vision_sniff_result", contentResult.Protocol())
		}
		return CompositeResult(metaresult, contentResult), nil
	}
	return contentResult, contentErr
}

func (d *DefaultDispatcher) routedDispatch(ctx context.Context, link *transport.Link, destination net.Destination) {
	outbounds := session.OutboundsFromContext(ctx)
	ob := outbounds[len(outbounds)-1]

	var handler outbound.Handler

	// Check if domain and protocol hit the rule
	sessionInbound := session.InboundFromContext(ctx)
	// Whether the inbound connection contains a user
	if sessionInbound.User != nil {
		if d.RuleManager.Detect(sessionInbound.Tag, destination.String(), sessionInbound.User.Email) {
			errors.LogError(ctx, fmt.Sprintf("User %s access %s reject by rule", sessionInbound.User.Email, destination.String()))
			newError("destination is reject by rule")
			common.Close(link.Writer)
			common.Interrupt(link.Reader)
			return
		}
	}

	routingLink := routingSession.AsRoutingContext(ctx)
	inTag := routingLink.GetInboundTag()
	isPickRoute := 0
	if forcedOutboundTag := session.GetForcedOutboundTagFromContext(ctx); forcedOutboundTag != "" {
		ctx = session.SetForcedOutboundTagToContext(ctx, "")
		if h := d.ohm.GetHandler(forcedOutboundTag); h != nil {
			isPickRoute = 1
			errors.LogInfo(ctx, "taking platform initialized detour [", forcedOutboundTag, "] for [", destination, "]")
			handler = h
		} else {
			errors.LogError(ctx, "non existing tag for platform initialized detour: ", forcedOutboundTag)
			common.Close(link.Writer)
			common.Interrupt(link.Reader)
			return
		}
	} else if d.router != nil {
		if route, err := d.router.PickRoute(routingLink); err == nil {
			outTag := route.GetOutboundTag()
			if h := d.ohm.GetHandler(outTag); h != nil {
				isPickRoute = 2
				errors.LogInfo(ctx, "taking detour [", outTag, "] for [", destination, "]")
				handler = h
			} else {
				errors.LogWarning(ctx, "non existing outTag: ", outTag)
			}
		} else {
			errors.LogInfo(ctx, "default route for ", destination)
		}
	}

	if handler == nil {
		handler = d.ohm.GetHandler(inTag) // Default outbound handler tag should be as same as the inbound tag
	}

	// If there is no outbound with tag as same as the inbound tag
	if handler == nil {
		handler = d.ohm.GetDefaultHandler()
	}

	if handler == nil {
		errors.LogInfo(ctx, "default outbound handler not exist")
		common.Close(link.Writer)
		common.Interrupt(link.Reader)
		return
	}

	ob.Tag = handler.Tag()

	if accessMessage := log.AccessMessageFromContext(ctx); accessMessage != nil {
		if tag := handler.Tag(); tag != "" {
			if inTag == "" {
				accessMessage.Detour = tag
			} else if isPickRoute == 1 {
				accessMessage.Detour = inTag + " ==> " + tag
			} else if isPickRoute == 2 {
				accessMessage.Detour = inTag + " -> " + tag
			} else {
				accessMessage.Detour = inTag + " >> " + tag
			}
		}
		log.Record(accessMessage)
	}

	if err := ctx.Err(); err != nil {
		errors.LogDebug(ctx, "vision_outbound_dispatch_main_ctx_done_before_handler", err)
	} else {
		errors.LogDebug(ctx, "vision_outbound_dispatch_main_ctx_ok_before_handler")
	}

	handler.Dispatch(ctx, link)
}
