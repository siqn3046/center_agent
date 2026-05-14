package mydispatcher

import "sync/atomic"

var disableVisionSniffReplay atomic.Bool

// SetDisableVisionSniffReplay 在创建 core.Instance 之前调用。为 true 时，即使入站 Sniffing 仍开启，
// mydispatcher 也会跳过 cachedReader / sniffer / replay，与「关闭 sniff」时相同，用于对照 Vision 问题。
func SetDisableVisionSniffReplay(v bool) {
	disableVisionSniffReplay.Store(v)
}

func visionSniffReplayDisabled() bool {
	return disableVisionSniffReplay.Load()
}
