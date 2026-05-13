const api = (path, opts = {}) => {
  const h = Object.assign({ "Content-Type": "application/json" }, opts.headers || {});
  const t = localStorage.getItem("center_jwt");
  if (t) h["Authorization"] = "Bearer " + t;
  return fetch(path, Object.assign({}, opts, { headers: h })).then(async (r) => {
    const txt = await r.text();
    let j = null;
    try { j = txt ? JSON.parse(txt) : null; } catch (_) {}
    if (!r.ok) throw new Error((j && j.message) || txt || r.statusText);
    return j;
  });
};

function render() {
  const root = document.getElementById("app");
  if (!localStorage.getItem("center_jwt")) {
    root.innerHTML = `<h2>登录</h2>
      <input id="u" placeholder="用户名" value="admin"/>
      <input id="p" type="password" placeholder="密码" value="admin123"/>
      <button id="login">登录</button>`;
    document.getElementById("login").onclick = async () => {
      const username = document.getElementById("u").value;
      const password = document.getElementById("p").value;
      const j = await api("/api/admin/login", { method: "POST", body: JSON.stringify({ username, password }) });
      localStorage.setItem("center_jwt", j.token);
      render();
    };
    return;
  }
  root.innerHTML = `<p><button id="logout">退出</button></p>
    <h2>节点</h2>
    <button id="reload">刷新列表</button>
    <button id="newn">创建节点</button>
    <div id="list"></div>
    <div id="detail"></div>`;
  document.getElementById("logout").onclick = () => { localStorage.removeItem("center_jwt"); render(); };
  const load = async () => {
    const nodes = await api("/api/admin/nodes");
    document.getElementById("list").innerHTML = "<table border=1><tr><th>ID</th><th>代码</th><th>名称</th><th>install_state</th><th>manage_status</th><th>操作</th></tr>" +
      nodes.map(n => `<tr><td>${n.id}</td><td>${n.node_code}</td><td>${n.node_name}</td><td>${n.install_state||""}</td><td>${n.manage_status||""}</td>
        <td><button data-id="${n.id}">详情</button> <button data-sid="${n.id}">安装脚本</button></td></tr>`).join("") + "</table>";
    root.querySelectorAll("button[data-id]").forEach(b => b.onclick = () => showDetail(b.getAttribute("data-id")));
    root.querySelectorAll("button[data-sid]").forEach(b => b.onclick = () => showScript(b.getAttribute("data-sid")));
  };
  document.getElementById("reload").onclick = load;
  document.getElementById("newn").onclick = async () => {
    const code = prompt("node_code（唯一）");
    const name = prompt("node_name");
    if (!code || !name) return;
    const j = await api("/api/admin/nodes", { method: "POST", body: JSON.stringify({ node_code: code, node_name: name }) });
    alert("请保存一次性 register_token：\n" + j.register_token + "\n\nnode_id=" + j.node_id);
    await load();
  };
  async function showDetail(id) {
    const n = await api("/api/admin/nodes/" + id);
    const dr = await api("/api/admin/nodes/" + id + "/discovery-reports");
    document.getElementById("detail").innerHTML = "<h3>节点 " + id + "</h3><pre>" + JSON.stringify(n, null, 2) + "</pre><h4>discovery_report</h4><pre>" + JSON.stringify(dr, null, 2) + "</pre>" +
      `<p><button id="ew">切换为 MANAGED_WRITABLE（测试）</button></p>`;
    document.getElementById("ew").onclick = async () => {
      await api("/api/admin/nodes/" + id + "/enable-writable", { method: "POST", body: "{}" });
      alert("已切换");
      showDetail(id);
    };
  }
  async function showScript(id) {
    const tok = prompt("粘贴创建节点时获得的 register_token（仅本地展示用）");
    if (!tok) return;
    const r = await fetch("/api/admin/nodes/" + id + "/install-script.sh?register_token=" + encodeURIComponent(tok), { headers: { Authorization: "Bearer " + localStorage.getItem("center_jwt") } });
    const t = await r.text();
    prompt("安装脚本（复制到 VPS root 执行）", t);
  }
  load();
}

document.addEventListener("DOMContentLoaded", render);
