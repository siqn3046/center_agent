(function () {
  "use strict";
  const TOKEN_KEY = "center_jwt";
  let timers = [];

  function esc(s) {
    if (s == null || s === undefined) return "";
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  /** 命令 result_json 摘要（不含 progress 数组，避免与「进度」列重复） */
  function formatCmdResult(rj) {
    if (!rj || typeof rj !== "object") return "";
    var parts = [];
    if (rj.stage != null && String(rj.stage) !== "") parts.push("stage: " + String(rj.stage));
    if (rj.message != null && String(rj.message) !== "") parts.push("message: " + String(rj.message));
    if (rj.error != null && String(rj.error) !== "") parts.push("error: " + String(rj.error));
    var rest = {};
    for (var k in rj) {
      if (!Object.prototype.hasOwnProperty.call(rj, k)) continue;
      if (k === "progress") continue;
      rest[k] = rj[k];
    }
    var skip = { stage: 1, message: 1, error: 1, progress: 1 };
    var keys = Object.keys(rest).filter(function (k) {
      return !skip[k];
    });
    if (keys.length) {
      var o = {};
      keys.forEach(function (k) {
        o[k] = rest[k];
      });
      try {
        parts.push(JSON.stringify(o));
      } catch (e) {
        parts.push("[result]");
      }
    }
    var s = parts.join(" | ");
    if (s.length > 560) return s.slice(0, 560) + "…";
    return s;
  }

  function toast(msg, err) {
    const w = document.getElementById("toasts");
    const el = document.createElement("div");
    el.className = "toast" + (err ? " err" : "");
    el.textContent = msg;
    w.appendChild(el);
    setTimeout(function () {
      el.remove();
    }, 4200);
  }

  function confirmModal(message, onOk) {
    const root = document.getElementById("modal-root");
    root.innerHTML =
      '<div class="modal-bg"><div class="modal"><h3>确认</h3><p class="small">' +
      esc(message) +
      '</p><div class="actions"><button class="btn" type="button" id="m-cancel">取消</button><button class="btn btn-danger" type="button" id="m-ok">确认</button></div></div></div>';
    root.querySelector("#m-cancel").onclick = function () {
      root.innerHTML = "";
    };
    root.querySelector("#m-ok").onclick = function () {
      root.innerHTML = "";
      onOk();
    };
  }

  function copyTextToClipboard(text) {
    if (!text) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(
        function () {
          toast("已复制到剪贴板");
        },
        function () {
          toast("复制失败", true);
        }
      );
      return;
    }
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.left = "-9999px";
    document.body.appendChild(ta);
    ta.select();
    try {
      document.execCommand("copy");
      toast("已复制到剪贴板");
    } catch (e) {
      toast("复制失败", true);
    }
    document.body.removeChild(ta);
  }

  function showAgentInstallModal(title, wget, curl, optNodeId) {
    const root = document.getElementById("modal-root");
    var goBtn =
      optNodeId != null
        ? '<p><button class="btn" type="button" id="miGo">打开节点详情</button></p>'
        : "";
    root.innerHTML =
      '<div class="modal-bg"><div class="modal" style="max-width:640px"><h3>' +
      esc(title) +
      '</h3><p class="small">请在目标 Linux VPS 上以 root 或 sudo 执行下列任一命令完成 Agent 安装与注册（register token 仅出现在命令中，勿泄露）。</p>' +
      '<label class="small">wget</label><textarea readonly class="code" id="miWget" rows="3" style="width:100%">' +
      esc(wget || "") +
      '</textarea><p><button class="btn" type="button" id="miCpW">复制 wget</button></p>' +
      '<label class="small">curl</label><textarea readonly class="code" id="miCurl" rows="3" style="width:100%">' +
      esc(curl || "") +
      '</textarea><p><button class="btn" type="button" id="miCpC">复制 curl</button></p>' +
      goBtn +
      '<div class="actions"><button class="btn btn-primary" type="button" id="miClose">关闭</button></div></div></div>';
    root.querySelector("#miClose").onclick = function () {
      root.innerHTML = "";
    };
    root.querySelector("#miCpW").onclick = function () {
      copyTextToClipboard((root.querySelector("#miWget") || {}).value || "");
    };
    root.querySelector("#miCpC").onclick = function () {
      copyTextToClipboard((root.querySelector("#miCurl") || {}).value || "");
    };
    var go = root.querySelector("#miGo");
    if (go && optNodeId != null) {
      go.onclick = function () {
        root.innerHTML = "";
        state.nodeId = optNodeId;
        state.view = "detail";
        state.tab = "overview";
        bootDetail();
      };
    }
  }

  function api(path, opts) {
    opts = opts || {};
    const h = Object.assign({ "Content-Type": "application/json" }, opts.headers || {});
    const t = localStorage.getItem(TOKEN_KEY);
    if (t) h["Authorization"] = "Bearer " + t;
    return fetch(path, Object.assign({}, opts, { headers: h })).then(function (r) {
      return r.text().then(function (txt) {
        let j = null;
        if (txt) {
          try {
            j = JSON.parse(txt);
          } catch (e) {}
        }
        if (!r.ok) {
          const msg = (j && (j.message || j.error)) || txt || r.statusText;
          throw new Error(msg);
        }
        return j;
      });
    });
  }

  function clearTimers() {
    timers.forEach(clearInterval);
    timers = [];
  }

  function fmtBytes(n) {
    if (n == null || isNaN(n)) return "-";
    const x = Number(n);
    if (x < 1024) return x.toFixed(0) + " B";
    if (x < 1024 * 1024) return (x / 1024).toFixed(1) + " KB";
    if (x < 1024 * 1024 * 1024) return (x / 1024 / 1024).toFixed(2) + " MB";
    return (x / 1024 / 1024 / 1024).toFixed(2) + " GB";
  }

  function fmtSpeed(n) {
    if (n == null || isNaN(n) || n <= 0) return "-";
    return fmtBytes(n) + "/s";
  }

  function fmtTime(iso) {
    if (!iso) return "-";
    try {
      return new Date(iso).toLocaleString();
    } catch (e) {
      return iso;
    }
  }

  function writable(node) {
    return node && node.manage_status === "MANAGED_WRITABLE";
  }

  function truthy(v) {
    return v === true || v === 1;
  }

  function artifactEnabled(x) {
    if (x == null) return false;
    if (x.enabled === false) return false;
    if (x.disabled === true) return false;
    return true;
  }

  function archMatchNode(agentArch, artArch) {
    if (!agentArch || !artArch) return false;
    var a = String(agentArch).toLowerCase();
    var b = String(artArch).toLowerCase();
    if (a === b) return true;
    if ((a === "amd64" && b === "x86_64") || (a === "x86_64" && b === "amd64")) return true;
    return false;
  }

  function artifactsForNode(n, list) {
    if (!list || !n || !n.agent_os || !n.agent_arch) return [];
    return list.filter(function (x) {
      if (!artifactEnabled(x)) return false;
      return String(x.os || x.target_os).toLowerCase() === String(n.agent_os).toLowerCase() && archMatchNode(n.agent_arch, x.arch || x.target_arch);
    });
  }

  const state = {
    view: "login",
    nodeId: null,
    tab: "overview",
    grid: true,
    q: "",
    summary: null,
    nodes: [],
    node: null,
    monitors: [],
    discovery: [],
    versions: [],
    deployTasks: [],
    commands: [],
    configDraft: "",
    configEdit: false,
    selectedVersionId: null,
    loading: false,
    artifacts: [],
  };

  function setLoading(v) {
    state.loading = v;
    const app = document.getElementById("app");
    if (app) app.classList.toggle("loading", v);
  }

  function loadSummary() {
    return api("/api/admin/dashboard/summary").then(function (s) {
      state.summary = s;
    });
  }

  function loadArtifacts() {
    return api("/api/admin/artifacts").then(function (a) {
      state.artifacts = a || [];
    });
  }

  function loadNodes() {
    const q = state.q ? "?q=" + encodeURIComponent(state.q) : "";
    return api("/api/admin/nodes" + q).then(function (n) {
      state.nodes = n || [];
    });
  }

  function loadNodeDetail() {
    if (!state.nodeId) return Promise.resolve();
    return Promise.all([
      api("/api/admin/nodes/" + state.nodeId).then(function (n) {
        state.node = n;
      }),
      api("/api/admin/nodes/" + state.nodeId + "/monitor-snapshots").then(function (m) {
        state.monitors = m || [];
      }),
      api("/api/admin/nodes/" + state.nodeId + "/discovery-reports").then(function (d) {
        state.discovery = d || [];
      }),
      api("/api/admin/nodes/" + state.nodeId + "/config-versions").then(function (v) {
        state.versions = v || [];
      }),
      api("/api/admin/nodes/" + state.nodeId + "/config-deploy-tasks").then(function (t) {
        state.deployTasks = t || [];
      }),
      api("/api/admin/nodes/" + state.nodeId + "/commands").then(function (c) {
        state.commands = c || [];
      }),
      api("/api/admin/artifacts").then(function (a) {
        state.artifacts = a || [];
      }),
    ]).then(function () {
      if (state.tab === "config" && state.node && state.node.imported_config_version_id) {
        return api("/api/admin/config-versions/" + state.node.imported_config_version_id).then(function (cv) {
          if (!state.configEdit) state.configDraft = cv.content_yaml || "";
          state.selectedVersionId = cv.id;
        });
      }
    });
  }

  function renderLogin() {
    clearTimers();
    document.getElementById("app").innerHTML =
      '<div class="panel" style="max-width:400px;margin:48px auto">' +
      "<h2 style=\"margin-top:0\">登录 XrayR Center</h2>" +
      '<p class="small">默认账号 admin / admin123（生产环境请立即修改）</p>' +
      '<label class="small">用户名</label><input class="input" id="lu" style="width:100%;margin-bottom:10px" value="admin"/>' +
      '<label class="small">密码</label><input class="input" id="lp" type="password" style="width:100%;margin-bottom:14px" value="admin123"/>' +
      '<button class="btn btn-primary" id="lb">登录</button></div>';
    document.getElementById("lb").onclick = function () {
      const u = document.getElementById("lu").value;
      const p = document.getElementById("lp").value;
      api("/api/admin/login", { method: "POST", body: JSON.stringify({ username: u, password: p }) })
        .then(function (j) {
          localStorage.setItem(TOKEN_KEY, j.token);
          state.view = "dashboard";
          toast("登录成功");
          bootDashboard();
        })
        .catch(function (e) {
          toast(e.message, true);
        });
    };
  }

  function badgeOnline(st) {
    return st === "online" ? "online" : "offline";
  }

  function badgeManage(ms) {
    if (ms === "MANAGED_WRITABLE") return "rw";
    if (ms === "MANAGED_READONLY") return "ro";
    if (ms === "REPAIR_REQUIRED") return "broken";
    return "noinst";
  }

  function badgeInstall(ins) {
    if (!ins) return "noinst";
    if (ins === "INSTALLED_RUNNING") return "online";
    if (ins.indexOf("BROKEN") >= 0 || ins === "SERVICE_ONLY") return "broken";
    if (ins === "NOT_INSTALLED") return "noinst";
    return "ro";
  }

  function renderDashboard() {
    const s = state.summary || {};
    const cards = state.nodes
      .map(function (n) {
        const ob = badgeOnline(n.online_status);
        const mg = badgeManage(n.manage_status);
        const ig = badgeInstall(n.install_state);
        const cpu = Math.round(Number(n.cpu_percent) || 0);
        const ram = Math.round(Number(n.ram_percent) || 0);
        const disk = Math.round(Number(n.disk_percent) || 0);
        return (
          '<div class="node-card" data-nid="' +
          n.id +
          '">' +
          '<div class="row1"><div><div class="title">' +
          esc(n.name || n.node_code) +
          '</div><div class="small">' +
          esc(n.region || "") +
          " · " +
          esc(n.public_ip || n.hostname || "") +
          "</div></div>" +
          '<div><span class="badge ' +
          ob +
          '">' +
          esc(n.online_status) +
          '</span> <span class="badge ' +
          mg +
          '">' +
          esc(n.manage_status || "") +
          '</span> <span class="badge ' +
          ig +
          '">' +
          esc(n.install_state || "") +
          "</span></div></div>" +
          '<div class="metrics"><div>CPU<br/><div class="meter"><i style="width:' +
          cpu +
          '%"></i></div></div><div>内存<br/><div class="meter"><i style="width:' +
          ram +
          '%"></i></div></div></div>' +
          '<div class="metrics" style="margin-top:8px"><div>上行 ' +
          esc(fmtSpeed(n.net_up_speed)) +
          '</div><div>下行 ' +
          esc(fmtSpeed(n.net_down_speed)) +
          "</div></div>" +
          '<div class="small" style="margin-top:8px">XrayR: ' +
          (n.xrayr_running ? "运行中" : "未运行/未知") +
          " · 心跳 " +
          esc(fmtTime(n.last_heartbeat_at)) +
          "</div></div>"
        );
      })
      .join("");

    const artRows = (state.artifacts || [])
      .map(function (a) {
        var nm = a.name || a.display_name || "";
        var ver = a.version || a.version_label || "";
        var osStr = a.os || a.target_os || "";
        var arch = a.arch || a.target_arch || "";
        var shaFull = a.sha256 || "";
        var shaShort = shaFull.length > 18 ? shaFull.slice(0, 18) + "…" : shaFull;
        var en = artifactEnabled(a);
        var sz = a.size_bytes != null && a.size_bytes !== "" ? String(a.size_bytes) : "-";
        return (
          "<tr><td>" +
          (a.id || "") +
          "</td><td>" +
          esc(nm) +
          "</td><td>" +
          esc(ver) +
          "</td><td>" +
          esc(osStr) +
          "</td><td>" +
          esc(arch) +
          "</td><td class=\"small\" title=\"" +
          esc(shaFull) +
          "\">" +
          esc(shaShort) +
          "</td><td>" +
          esc(sz) +
          "</td><td>" +
          (en ? "启用" : "停用") +
          '</td><td><button type="button" class="btn btn-ghost btn-art-toggle" data-art-id="' +
          esc(String(a.id)) +
          '" data-next="' +
          (en ? "0" : "1") +
          '">' +
          (en ? "停用" : "启用") +
          "</button></td></tr>"
        );
      })
      .join("");
    const artifactPanel =
      '<div class="panel" style="margin:12px 16px">' +
      "<h3 style=\"margin-top:0\">制品管理（XrayR 二进制）</h3>" +
      '<p class="small">登记前文件须已位于 CENTER_ARTIFACT_DIR 下；上传会写入 <code>版本/linux-架构/</code> 目录。sha256 留空则上传后由服务端计算。</p>' +
      '<div style="display:flex;flex-wrap:wrap;gap:16px;align-items:flex-start">' +
      '<form id="formArtUpload" style="flex:1;min-width:280px" class="stack">' +
      "<b>上传并登记</b>" +
      '<input class="input" name="name" placeholder="name" required />' +
      '<input class="input" name="version" placeholder="version" required />' +
      '<input type="hidden" name="os" value="linux" />' +
      '<label class="small">架构</label><select class="input" name="arch"><option value="amd64">amd64</option><option value="arm64">arm64</option></select>' +
      '<input class="input" name="sha256" placeholder="sha256（可选，留空则自动计算）" />' +
      '<input class="input" type="file" name="file" required />' +
      '<button class="btn btn-primary" type="submit">上传</button>' +
      "</form>" +
      '<div style="flex:1;min-width:280px" class="stack">' +
      "<b>仅登记（磁盘上已有文件）</b>" +
      '<input class="input" id="reg_name" placeholder="name" />' +
      '<input class="input" id="reg_ver" placeholder="version" />' +
      '<select class="input" id="reg_arch"><option value="amd64">amd64</option><option value="arm64">arm64</option></select>' +
      '<input class="input" id="reg_sha" placeholder="sha256（必填）" />' +
      '<input class="input" id="reg_fn" placeholder="filename（与 relpath 二选一）" />' +
      '<input class="input" id="reg_rel" placeholder="storage_relpath（相对制品目录）" />' +
      '<button class="btn" type="button" id="btnRegArt">登记</button>' +
      "</div></div>" +
      '<div style="overflow:auto"><table class="data"><thead><tr><th>ID</th><th>名称</th><th>版本</th><th>OS</th><th>arch</th><th>sha256</th><th>大小(bytes)</th><th>状态</th><th>操作</th></tr></thead><tbody>' +
      artRows +
      "</tbody></table></div></div>";

    document.getElementById("app").innerHTML =
      '<div class="topbar"><div class="brand">XrayR Center</div><div><button class="btn" id="logout">退出</button></div></div>' +
      '<div class="grid-stats">' +
      '<div class="stat-card"><div class="label">服务器时间</div><div class="value small">' +
      esc(s.server_time || "") +
      "</div></div>" +
      '<div class="stat-card"><div class="label">在线 / 总数</div><div class="value">' +
      esc(String(s.online_nodes)) +
      " / " +
      esc(String(s.total_nodes)) +
      "</div></div>" +
      '<div class="stat-card"><div class="label">区域数</div><div class="value">' +
      esc(String(s.region_count || 0)) +
      "</div></div>" +
      '<div class="stat-card"><div class="label">总下行速度</div><div class="value small">' +
      esc(fmtSpeed(s.total_speed_down)) +
      "</div></div>" +
      '<div class="stat-card"><div class="label">总上行速度</div><div class="value small">' +
      esc(fmtSpeed(s.total_speed_up)) +
      "</div></div>" +
      '<div class="stat-card"><div class="label">累计下行 / 上行流量</div><div class="value small">' +
      esc(fmtBytes(s.total_traffic_down)) +
      " / " +
      esc(fmtBytes(s.total_traffic_up)) +
      "</div></div></div>" +
      '<div class="toolbar"><input class="input" id="sq" placeholder="搜索节点名称、IP、地区、系统…" value="' +
      esc(state.q) +
      '"/>' +
      '<button class="btn" id="sgo">搜索</button>' +
      '<div class="toggle"><button type="button" id="vgrid" class="' +
      (state.grid ? "active" : "") +
      '">卡片</button><button type="button" id="vtable" class="' +
      (!state.grid ? "active" : "") +
      '">表格</button></div>' +
      '<button class="btn btn-primary" id="newnode">创建节点</button></div>' +
      artifactPanel +
      (state.grid
        ? '<div class="cards" id="cardbox">' + cards + "</div>"
        : '<div class="panel" style="overflow:auto"><table class="data"><thead><tr><th>ID</th><th>名称</th><th>在线</th><th>CPU</th><th>内存</th><th>管理</th><th>安装状态</th></tr></thead><tbody id="tbody"></tbody></table></div>') +
      "";

    document.getElementById("logout").onclick = function () {
      localStorage.removeItem(TOKEN_KEY);
      state.view = "login";
      renderLogin();
    };
    document.getElementById("sgo").onclick = function () {
      state.q = document.getElementById("sq").value;
      refreshDash();
    };
    document.getElementById("vgrid").onclick = function () {
      state.grid = true;
      refreshDash();
    };
    document.getElementById("vtable").onclick = function () {
      state.grid = false;
      refreshDash();
    };
    document.getElementById("newnode").onclick = function () {
      const code = prompt("node_code（唯一）");
      const name = prompt("node_name");
      if (!code || !name) return;
      api("/api/admin/nodes", { method: "POST", body: JSON.stringify({ node_code: code, node_name: name }) })
        .then(function (j) {
          showAgentInstallModal("节点已创建", j.install_command_wget || "", j.install_command_curl || "", j.node_id);
          refreshDash();
        })
        .catch(function (e) {
          toast(e.message, true);
        });
    };

    var formArtUp = document.getElementById("formArtUpload");
    if (formArtUp) {
      formArtUp.onsubmit = function (ev) {
        ev.preventDefault();
        var fd = new FormData(formArtUp);
        var sh = (fd.get("sha256") || "").toString().trim();
        if (!sh) fd.delete("sha256");
        var tok = localStorage.getItem(TOKEN_KEY);
        fetch("/api/admin/artifacts/upload", {
          method: "POST",
          headers: tok ? { Authorization: "Bearer " + tok } : {},
          body: fd,
        })
          .then(function (r) {
            return r.text().then(function (txt) {
              var j = null;
              if (txt) {
                try {
                  j = JSON.parse(txt);
                } catch (e) {}
              }
              if (!r.ok) {
                throw new Error((j && (j.message || j.error)) || txt || r.statusText);
              }
              return j;
            });
          })
          .then(function (j) {
            toast("上传成功，id=" + (j && j.id) + (j && j.sha256 ? " sha256=" + j.sha256.slice(0, 16) + "…" : ""));
            formArtUp.reset();
            return refreshDash();
          })
          .catch(function (e) {
            toast(e.message, true);
          });
      };
    }
    var btnRegArt = document.getElementById("btnRegArt");
    if (btnRegArt) {
      btnRegArt.onclick = function () {
        var payload = {
          name: document.getElementById("reg_name").value.trim(),
          version: document.getElementById("reg_ver").value.trim(),
          arch: document.getElementById("reg_arch").value,
          os: "linux",
          sha256: document.getElementById("reg_sha").value.trim(),
        };
        var fn = document.getElementById("reg_fn").value.trim();
        var rel = document.getElementById("reg_rel").value.trim();
        if (fn) payload.filename = fn;
        if (rel) payload.storage_relpath = rel;
        if (!payload.name || !payload.version || !payload.sha256) {
          toast("请填写 name、version、sha256", true);
          return;
        }
        api("/api/admin/artifacts", { method: "POST", body: JSON.stringify(payload) })
          .then(function () {
            toast("已登记制品");
            document.getElementById("reg_name").value = "";
            document.getElementById("reg_ver").value = "";
            document.getElementById("reg_sha").value = "";
            document.getElementById("reg_fn").value = "";
            document.getElementById("reg_rel").value = "";
            return refreshDash();
          })
          .catch(function (e) {
            toast(e.message, true);
          });
      };
    }
    document.querySelectorAll(".btn-art-toggle").forEach(function (b) {
      b.onclick = function () {
        var id = b.getAttribute("data-art-id");
        var nextEn = b.getAttribute("data-next") === "1";
        api("/api/admin/artifacts/" + id, {
          method: "PATCH",
          body: JSON.stringify({ enabled: nextEn }),
        })
          .then(function () {
            toast(nextEn ? "已启用" : "已停用");
            return refreshDash();
          })
          .catch(function (e) {
            toast(e.message, true);
          });
      };
    });

    document.querySelectorAll(".node-card").forEach(function (el) {
      el.onclick = function () {
        state.nodeId = parseInt(el.getAttribute("data-nid"), 10);
        state.view = "detail";
        state.tab = "overview";
        bootDetail();
      };
    });

    if (!state.grid) {
      const tb = document.getElementById("tbody");
      if (tb) {
        tb.innerHTML = state.nodes
          .map(function (n) {
            return (
              "<tr data-nid=\"" +
              n.id +
              '" style="cursor:pointer"><td>' +
              n.id +
              "</td><td>" +
              esc(n.name) +
              "</td><td>" +
              esc(n.online_status) +
              "</td><td>" +
              Math.round(n.cpu_percent || 0) +
              "%</td><td>" +
              Math.round(n.ram_percent || 0) +
              "%</td><td>" +
              esc(n.manage_status) +
              "</td><td>" +
              esc(n.install_state) +
              "</td></tr>"
            );
          })
          .join("");
        tb.querySelectorAll("tr").forEach(function (r) {
          r.onclick = function () {
            state.nodeId = parseInt(r.getAttribute("data-nid"), 10);
            state.view = "detail";
            state.tab = "overview";
            bootDetail();
          };
        });
      }
    }
  }

  function refreshDash() {
    setLoading(true);
    Promise.all([loadSummary(), loadNodes(), loadArtifacts()])
      .then(function () {
        renderDashboard();
      })
      .catch(function (e) {
        toast(e.message, true);
      })
      .finally(function () {
        setLoading(false);
      });
  }

  function bootDashboard() {
    clearTimers();
    refreshDash();
    timers.push(
      setInterval(function () {
        if (state.view === "dashboard") refreshDash();
      }, 10000)
    );
  }

  function tabBar() {
    const tabs = ["overview", "discovery", "config", "commands", "settings"];
    const labels = { overview: "概览", discovery: "Discovery", config: "XrayR 配置", commands: "命令", settings: "设置" };
    return (
      '<div class="tabs">' +
      tabs
        .map(function (t) {
          return (
            '<button type="button" data-tab="' +
            t +
            '" class="' +
            (state.tab === t ? "active" : "") +
            '">' +
            labels[t] +
            "</button>"
          );
        })
        .join("") +
      "</div>"
    );
  }

  function renderDetail() {
    const n = state.node || {};
    const body =
      '<div class="topbar"><div><button class="btn" id="back">← 返回</button> <span class="brand">' +
      esc(n.node_name || "") +
      "</span> <span class=\"small\">ID " +
      esc(String(n.id)) +
      "</span></div><div><button class="btn" id="logout2">退出</button></div></div>" +
      '<div class="detail-header"><div><span class="badge ' +
      badgeManage(n.manage_status) +
      '">' +
      esc(n.manage_status || "") +
      '</span> <span class="badge ' +
      badgeInstall(n.install_state) +
      '">' +
      esc(n.install_state || "") +
      "</span></div>" +
      '<button class="btn" id="script">高级：install-script.sh</button></div>' +
      tabBar() +
      '<div id="tabbody"></div>';

    document.getElementById("app").innerHTML = body;
    document.getElementById("back").onclick = function () {
      state.view = "dashboard";
      bootDashboard();
    };
    document.getElementById("logout2").onclick = function () {
      localStorage.removeItem(TOKEN_KEY);
      state.view = "login";
      renderLogin();
    };
    document.getElementById("script").onclick = function () {
      const tok = prompt("粘贴 register_token");
      if (!tok) return;
      window.open("/api/admin/nodes/" + state.nodeId + "/install-script.sh?register_token=" + encodeURIComponent(tok));
    };
    document.querySelectorAll(".tabs button").forEach(function (b) {
      b.onclick = function () {
        state.tab = b.getAttribute("data-tab");
        renderDetail();
        renderTab();
      };
    });
    renderTab();
  }

  function renderTab() {
    const el = document.getElementById("tabbody");
    if (!el) return;
    const n = state.node || {};
    if (state.tab === "overview") {
      const last = state.monitors[0] || {};
      const pay = last.payload_json || {};
      const wget = n.install_command_wget || "";
      const curl = n.install_command_curl || "";
      const hint = n.install_command_hint || "";
      el.innerHTML =
        '<div class="panel"><h3 style="margin-top:0">概览</h3><p class="small">最近心跳：' +
        fmtTime(n.last_seen_at) +
        "</p>" +
        "<p>CPU " +
        (last.cpu_pct != null ? last.cpu_pct.toFixed(1) : "-") +
        "% · 内存 " +
        (last.mem_pct != null ? last.mem_pct.toFixed(1) : "-") +
        "% · 磁盘 " +
        (last.disk_pct != null ? last.disk_pct.toFixed(1) : "-") +
        "%</p>" +
        '<div class="panel" style="margin-top:14px;border:1px solid rgba(91,140,255,0.35)">' +
        "<h4 style=\"margin-top:0\">Agent 一键安装</h4>" +
        '<p class="small">在目标机器上以 root 或 sudo 执行（脚本：<code>/install-agent.sh</code>）。' +
        "安装后 Agent 会向 Center 注册；请在 Center 配置 <code>CENTER_AGENT_DOWNLOAD_URL</code> 与 <code>CENTER_AGENT_SHA256</code>（或按文档放置制品），否则脚本无法下载二进制。</p>" +
        '<p class="small">' +
        esc(hint) +
        "</p>" +
        '<label class="small">wget</label><textarea readonly class="code" id="instWget" rows="3" style="width:100%;resize:vertical">' +
        esc(wget) +
        '</textarea><p><button type="button" class="btn" id="cpInstW">复制 wget</button></p>' +
        '<label class="small">curl</label><textarea readonly class="code" id="instCurl" rows="3" style="width:100%;resize:vertical">' +
        esc(curl) +
        '</textarea><p><button type="button" class="btn" id="cpInstC">复制 curl</button> ' +
        '<button type="button" class="btn btn-primary" id="issueInstTok">生成新的安装令牌</button></p>' +
        "</div>" +
        '<h4 class="small" style="margin-top:16px">监控 JSON（最近一条）</h4>' +
        '<pre class="small" style="white-space:pre-wrap;font-size:12px">' +
        esc(JSON.stringify(pay, null, 2)) +
        "</pre></div>";
      document.getElementById("cpInstW").onclick = function () {
        copyTextToClipboard(wget);
      };
      document.getElementById("cpInstC").onclick = function () {
        copyTextToClipboard(curl);
      };
      document.getElementById("issueInstTok").onclick = function () {
        api("/api/admin/nodes/" + state.nodeId + "/issue-install-token", { method: "POST", body: "{}" })
          .then(function (j) {
            state.node.install_command_wget = j.install_command_wget;
            state.node.install_command_curl = j.install_command_curl;
            state.node.expires_at = j.expires_at;
            toast("已生成新安装命令" + (j.expires_at ? "，到期 " + j.expires_at : ""));
            renderTab();
          })
          .catch(function (e) {
            toast(e.message, true);
          });
      };
      return;
    }
    if (state.tab === "discovery") {
      el.innerHTML =
        '<div class="panel"><h3 style="margin-top:0">Discovery</h3><button class="btn" id="drf">刷新</button><div style="margin-top:12px">' +
        state.discovery
          .map(function (d) {
            const st = d.install_state || "";
            let cls = "noinst";
            if (st === "INSTALLED_RUNNING") cls = "online";
            else if (st.indexOf("BROKEN") >= 0 || st === "SERVICE_ONLY") cls = "broken";
            return (
              '<div class="panel" style="margin-bottom:10px;border-left:4px solid rgba(91,140,255,0.5)">' +
              '<span class="badge ' +
              cls +
              '">' +
              esc(st) +
              "</span> " +
              esc(fmtTime(d.last_report_time || d.created_at)) +
              "<pre class=\"small\" style=\"white-space:pre-wrap\">" +
              esc(d.error_tail || "") +
              "</pre></div>"
            );
          })
          .join("") +
        "</div></div>";
      document.getElementById("drf").onclick = function () {
        loadNodeDetail().then(renderDetail);
      };
      return;
    }
    if (state.tab === "config") {
      const ro = state.configEdit ? "" : "readonly";
      el.innerHTML =
        '<div class="panel"><h3 style="margin-top:0">XrayR 配置</h3>' +
        '<p class="small">当前导入版本 ID：' +
        esc(String(n.imported_config_version_id || "-")) +
        " · hash：" +
        esc(n.imported_config_hash || "-") +
        '</p><p><button class="btn" id="tged">编辑</button> <button class="btn btn-primary" id="svcv">保存为新版本</button></p>' +
        '<label class="small">配置名称</label><input class="input" id="cvname" style="width:100%;margin-bottom:8px" placeholder="例如 manual-2026"/>' +
        '<label class="small">备注</label><input class="input" id="cvremark" style="width:100%;margin-bottom:8px"/>' +
        '<label class="small">YAML</label><textarea class="code" id="cvyaml" ' +
        ro +
        ">" +
        esc(state.configDraft) +
        "</textarea>" +
        '<h4>版本列表</h4><table class="data"><thead><tr><th>ID</th><th>版本</th><th>类型</th><th>hash</th><th>时间</th><th>操作</th></tr></thead><tbody>' +
        state.versions
          .map(function (v) {
            return (
              "<tr><td>" +
              v.id +
              "</td><td>" +
              esc(v.display_name || v.version) +
              "</td><td>" +
              esc(v.source_type) +
              "</td><td class=\"small\">" +
              esc((v.content_sha256 || "").slice(0, 12)) +
              "…</td><td>" +
              esc(fmtTime(v.created_at)) +
              '</td><td><button class="btn" data-vopen="' +
              v.id +
              '">查看</button> ' +
              (writable(n)
                ? '<button class="btn btn-primary" data-vdep="' + v.id + '">下发</button>'
                : "<span class=\"small\">只读</span>") +
              "</td></tr>"
            );
          })
          .join("") +
        "</tbody></table>" +
        "<h4>下发任务</h4><table class=\"data\"><thead><tr><th>ID</th><th>状态</th><th>备份路径</th><th>错误</th><th>完成时间</th></tr></thead><tbody>" +
        state.deployTasks
          .map(function (t) {
            return (
              "<tr><td>" +
              t.id +
              "</td><td>" +
              esc(t.status) +
              "</td><td class=\"small\">" +
              esc(t.backup_path || "") +
              "</td><td class=\"small\">" +
              esc(t.error_message || "") +
              "</td><td>" +
              esc(fmtTime(t.finished_at)) +
              "</td></tr>"
            );
          })
          .join("") +
        "</tbody></table></div>";

      document.getElementById("tged").onclick = function () {
        state.configEdit = !state.configEdit;
        renderDetail();
        renderTab();
      };
      document.getElementById("svcv").onclick = function () {
        const yaml = document.getElementById("cvyaml").value;
        const name = document.getElementById("cvname").value || "manual";
        const remark = document.getElementById("cvremark").value || "";
        if (!yaml.trim()) {
          toast("YAML 不能为空", true);
          return;
        }
        api("/api/admin/nodes/" + state.nodeId + "/config-versions", {
          method: "POST",
          body: JSON.stringify({ name: name, remark: remark, config_yaml: yaml }),
        })
          .then(function () {
            toast("已保存新版本");
            state.configEdit = false;
            return loadNodeDetail();
          })
          .then(renderDetail)
          .catch(function (e) {
            toast(e.message, true);
          });
      };
      document.querySelectorAll("button[data-vopen]").forEach(function (b) {
        b.onclick = function () {
          const id = b.getAttribute("data-vopen");
          api("/api/admin/config-versions/" + id).then(function (cv) {
            state.configDraft = cv.content_yaml || "";
            state.selectedVersionId = cv.id;
            state.configEdit = true;
            renderDetail();
            renderTab();
          });
        };
      });
      document.querySelectorAll("button[data-vdep]").forEach(function (b) {
        b.onclick = function () {
          const vid = b.getAttribute("data-vdep");
          confirmModal("确认将配置版本 " + vid + " 下发到本节点？将创建 APPLY_CONFIG 任务。", function () {
            api("/api/admin/nodes/" + state.nodeId + "/config-versions/" + vid + "/deploy", { method: "POST", body: "{}" })
              .then(function () {
                toast("已创建下发任务");
                return loadNodeDetail();
              })
              .then(renderDetail)
              .catch(function (e) {
                toast(e.message, true);
              });
          });
        };
      });
      return;
    }
    if (state.tab === "commands") {
      const arts = artifactsForNode(n, state.artifacts);
      const artOpts =
        '<option value="">-- 选择制品 --</option>' +
        arts
          .map(function (x) {
            var label =
              (x.name || x.display_name || "") +
              " " +
              (x.version || x.version_label || "") +
              (x.sha256 ? " · " + String(x.sha256).slice(0, 12) + "…" : "");
            return '<option value="' + esc(String(x.id)) + '">' + esc(label.trim() || "artifact " + x.id) + "</option>";
          })
          .join("");
      const noArtsHint =
        arts.length === 0
          ? '<p class="small" style="color:#b45309">无匹配制品：请确认节点已上报 agent_os/agent_arch，并在 Center 配置 CENTER_ARTIFACT_DIR 后登记制品（POST /api/admin/artifacts）。</p>'
          : "";
      const canRestart = truthy(n.allow_restart);
      const canApply = truthy(n.allow_config_apply);
      const canInstall = truthy(n.allow_install);
      const canUpgrade = truthy(n.allow_upgrade);
      const installDisabled = !canInstall || arts.length === 0;
      const upgradeDisabled = !canUpgrade || arts.length === 0;
      el.innerHTML =
        '<div class="panel"><h3 style="margin-top:0">命令</h3><div style="margin-bottom:10px">' +
        '<button class="btn" id="cst">查看状态</button> ' +
        (writable(n)
          ? '<button class="btn" id="crst" ' +
            (canRestart ? "" : "disabled title=\"需要 allow_restart\"") +
            '>重启 XrayR</button> <button class="btn" id="capp" ' +
            (canApply ? "" : "disabled title=\"需要 allow_config_apply\"") +
            '>应用配置（待选版本）</button>' +
            '<div style="margin-top:10px">' +
            '<label class="small">安装/升级使用的制品</label>' +
            '<select id="artsel" class="input" style="width:100%;max-width:480px">' +
            artOpts +
            "</select></div>" +
            noArtsHint +
            '<button class="btn btn-danger" style="margin-top:8px;margin-right:8px" id="cins" ' +
            (installDisabled ? "disabled" : "") +
            '>安装 XrayR</button>' +
            '<button class="btn btn-danger" id="cupg" ' +
            (upgradeDisabled ? "disabled" : "") +
            '>升级 XrayR</button>' +
            '</div><p class="small">MANAGED_READONLY 节点仅可使用「查看状态」。安装/升级为高危操作，需二次确认且后端校验 allow_install / allow_upgrade。</p>'
          : '<span class="small">MANAGED_READONLY：仅允许查看状态</span>') +
        '</div><table class="data"><thead><tr><th>command_id</th><th>类型</th><th>状态</th><th>进度</th><th>结果</th><th>错误</th><th>创建</th></tr></thead><tbody>' +
        state.commands
          .map(function (c) {
            var prog = "";
            if (c.result_json && c.result_json.progress && Array.isArray(c.result_json.progress)) {
              prog = c.result_json.progress
                .map(function (s) {
                  return (s.step || "") + ": " + (s.message || "");
                })
                .join(" | ");
            }
            if (!prog && c.log_summary) prog = c.log_summary;
            if (prog.length > 420) prog = prog.slice(0, 420) + "…";
            var resStr = formatCmdResult(c.result_json || {});
            return (
              "<tr><td class=\"small\">" +
              esc(c.command_id) +
              "</td><td>" +
              esc(c.command_type) +
              "</td><td>" +
              esc(c.status) +
              "</td><td class=\"small\">" +
              esc(prog) +
              "</td><td class=\"small\" title=\"" +
              esc(resStr) +
              "\">" +
              esc(resStr) +
              "</td><td class=\"small\">" +
              esc(c.error_message || "") +
              "</td><td class=\"small\">" +
              esc(fmtTime(c.created_at)) +
              "</td></tr>"
            );
          })
          .join("") +
        "</tbody></table></div>";

      document.getElementById("cst").onclick = function () {
        api("/api/admin/nodes/" + state.nodeId + "/commands/status-xrayr", { method: "POST", body: "{}" })
          .then(function () {
            toast("已下发 STATUS");
            return loadNodeDetail();
          })
          .then(renderDetail)
          .catch(function (e) {
            toast(e.message, true);
          });
      };
      if (writable(n)) {
        var rst = document.getElementById("crst");
        if (rst && canRestart) {
          rst.onclick = function () {
            confirmModal("确认重启 XrayR 服务？", function () {
              api("/api/admin/nodes/" + state.nodeId + "/commands/restart-xrayr", { method: "POST", body: "{}" })
                .then(function () {
                  toast("已下发 RESTART");
                  return loadNodeDetail();
                })
                .then(renderDetail)
                .catch(function (e) {
                  toast(e.message, true);
                });
            });
          };
        }
        var appb = document.getElementById("capp");
        if (appb && canApply) {
          appb.onclick = function () {
            var vid = n.pending_deploy_version_id || state.selectedVersionId;
            if (!vid) {
              toast("请先在设置中指定 pending_deploy_version_id 或于配置页选择版本", true);
              return;
            }
            confirmModal("确认下发配置版本 " + vid + "？", function () {
              api("/api/admin/nodes/" + state.nodeId + "/config-versions/" + vid + "/deploy", { method: "POST", body: "{}" })
                .then(function () {
                  toast("已创建 APPLY_CONFIG");
                  return loadNodeDetail();
                })
                .then(renderDetail)
                .catch(function (e) {
                  toast(e.message, true);
                });
            });
          };
        }
        var ins = document.getElementById("cins");
        if (ins && canInstall && !installDisabled) {
          ins.onclick = function () {
            var sel = document.getElementById("artsel");
            var aid = sel && parseInt(sel.value, 10);
            if (!aid) {
              toast("请选择制品", true);
              return;
            }
            confirmModal("确认在本节点安装 XrayR？将下载制品、校验校验和、写入 systemd 并重启服务。", function () {
              api("/api/admin/nodes/" + state.nodeId + "/commands/install-xrayr", {
                method: "POST",
                body: JSON.stringify({ artifact_id: aid }),
              })
                .then(function () {
                  toast("已下发 INSTALL_XRAYR");
                  return loadNodeDetail();
                })
                .then(renderDetail)
                .catch(function (e) {
                  toast(e.message, true);
                });
            });
          };
        }
        var upg = document.getElementById("cupg");
        if (upg && canUpgrade && !upgradeDisabled) {
          upg.onclick = function () {
            var sel = document.getElementById("artsel");
            var aid = sel && parseInt(sel.value, 10);
            if (!aid) {
              toast("请选择制品", true);
              return;
            }
            confirmModal("确认升级本节点 XrayR？将备份旧文件、替换二进制并重启服务。", function () {
              api("/api/admin/nodes/" + state.nodeId + "/commands/upgrade-xrayr", {
                method: "POST",
                body: JSON.stringify({ artifact_id: aid }),
              })
                .then(function () {
                  toast("已下发 UPGRADE_XRAYR");
                  return loadNodeDetail();
                })
                .then(renderDetail)
                .catch(function (e) {
                  toast(e.message, true);
                });
            });
          };
        }
      }
      return;
    }
    if (state.tab === "settings") {
      el.innerHTML =
        '<div class="panel"><h3 style="margin-top:0">节点设置</h3>' +
        '<p class="small">路径类仅展示 Agent 本地 agent.yml 建议值，Center 不会下发任意路径执行。</p>' +
        '<label class="small">节点名称</label><input class="input" id="snname" style="width:100%" value="' +
        esc(n.node_name || "") +
        '"/>' +
        '<label class="small">地区</label><input class="input" id="snreg" style="width:100%" value="' +
        esc(n.region || "") +
        '"/>' +
        '<label class="small">备注</label><input class="input" id="snrmk" style="width:100%" value="' +
        esc(n.remark || "") +
        '"/>' +
        '<label class="small">manage_status（MANAGED_READONLY / MANAGED_WRITABLE / DISCOVERED）</label><input class="input" id="snms" style="width:100%" value="' +
        esc(n.manage_status || "") +
        '"/>' +
        '<p class="small">布尔权限</p><label><input type="checkbox" id="sar" ' +
        (n.allow_restart ? "checked" : "") +
        '/> allow_restart</label><br/>' +
        '<label><input type="checkbox" id="sai" ' +
        (n.allow_install ? "checked" : "") +
        '/> allow_install（安装 XrayR）</label><br/>' +
        '<label><input type="checkbox" id="sac" ' +
        (n.allow_config_apply ? "checked" : "") +
        '/> allow_config_apply</label><br/>' +
        '<label><input type="checkbox" id="sau" ' +
        (n.allow_upgrade ? "checked" : "") +
        '/> allow_upgrade</label><br/>' +
        '<label class="small">pending_deploy_version_id</label><input class="input" id="snpv" style="width:100%" value="' +
        esc(n.pending_deploy_version_id != null ? String(n.pending_deploy_version_id) : "") +
        '"/>' +
        '<p style="margin-top:14px"><button class="btn btn-primary" id="ssave">保存</button> ' +
        '<button class="btn" id="swr">一键 enable-writable（测试）</button></p>' +
        '<hr/><p class="small">Agent 建议：service_name=xrayr；config_path=/etc/XrayR/config.yml；binary_path 由发现；backup_dir 与 artifact_cache_dir 见安装脚本。</p></div>';

      document.getElementById("ssave").onclick = function () {
        const body = {
          node_name: document.getElementById("snname").value,
          region: document.getElementById("snreg").value || null,
          remark: document.getElementById("snrmk").value || null,
          manage_status: document.getElementById("snms").value || null,
          allow_restart: document.getElementById("sar").checked,
          allow_install: document.getElementById("sai").checked,
          allow_config_apply: document.getElementById("sac").checked,
          allow_upgrade: document.getElementById("sau").checked,
        };
        const pv = document.getElementById("snpv").value.trim();
        if (pv) body.pending_deploy_version_id = parseInt(pv, 10);
        api("/api/admin/nodes/" + state.nodeId + "/settings", { method: "PATCH", body: JSON.stringify(body) })
          .then(function () {
            toast("已保存");
            return loadNodeDetail();
          })
          .then(renderDetail)
          .catch(function (e) {
            toast(e.message, true);
          });
      };
      document.getElementById("swr").onclick = function () {
        api("/api/admin/nodes/" + state.nodeId + "/enable-writable", { method: "POST", body: "{}" })
          .then(function () {
            toast("已切换 WRITABLE");
            return loadNodeDetail();
          })
          .then(renderDetail)
          .catch(function (e) {
            toast(e.message, true);
          });
      };
    }
  }

  function bootDetail() {
    clearTimers();
    setLoading(true);
    loadNodeDetail()
      .then(function () {
        renderDetail();
        timers.push(
          setInterval(function () {
            if (state.view === "detail") {
              loadNodeDetail().then(renderDetail);
            }
          }, 10000)
        );
        timers.push(
          setInterval(function () {
            if (state.view === "detail" && state.tab === "commands") {
              loadNodeDetail().then(renderDetail);
            }
          }, 3000)
        );
      })
      .catch(function (e) {
        toast(e.message, true);
      })
      .finally(function () {
        setLoading(false);
      });
  }

  function boot() {
    if (!localStorage.getItem(TOKEN_KEY)) {
      renderLogin();
      return;
    }
    state.view = "dashboard";
    bootDashboard();
  }

  boot();
})();
