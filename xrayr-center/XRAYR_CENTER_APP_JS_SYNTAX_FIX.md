# Center 前端 `app.js` 语法错误修复说明

## 修改文件

- `xrayr-center/internal/httpapi/static/app.js`（Center 内置 HTTP 静态资源；若部署路径为 `internal/static/app.js`，请以实际仓库目录为准）

## 根因

浏览器报错：`Uncaught SyntaxError: Unexpected identifier 'btn' at app.js:728:41`。

`renderDetail()` 中拼接 `body` 时，第 728 行片段使用了 **双引号包裹的 JavaScript 字符串**，其 HTML 内仍写为 `class="btn"`、`id="logout2"`。**内层双引号未转义**，解析器在 `class=` 后的第一个 `"` 处认为字符串已结束，随后的 `btn` 被当作代码，触发 `Unexpected identifier 'btn'`。

## 修复点

将 HTML 属性中的双引号改为在 JS 字符串内合法形式：`class=\"btn\"`、`id=\"logout2\"`。不改变任何业务逻辑与 DOM 结构，仅修复无法解析的语法错误。

## 验证方法

1. 用 Node 校验语法（可选）：`node --check internal/httpapi/static/app.js`（在 `xrayr-center` 目录下，路径按仓库为准）。
2. 启动 Center 管理界面，打开浏览器开发者工具 **Console**，进入节点详情页（触发 `renderDetail`），确认不再出现上述 `SyntaxError`。
3. 确认「返回」「退出」等按钮仍可正常点击。

---

文档与源码字符串编码：UTF-8。
