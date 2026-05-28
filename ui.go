package main

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
	<title>HTTP Sink Tap</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f7f7f4;
      --panel: #ffffff;
      --line: #d8d7d0;
      --text: #20211f;
      --muted: #686b64;
      --accent: #176b53;
      --warn: #a33f24;
      --mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #171815;
        --panel: #20221e;
        --line: #3a3d35;
        --text: #f0f1ec;
        --muted: #a9ada2;
        --accent: #62c29f;
        --warn: #f18a6d;
      }
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      color: var(--text);
      background: var(--bg);
    }
    header {
      height: 56px;
      border-bottom: 1px solid var(--line);
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 0 18px;
      gap: 16px;
    }
    h1 {
      font-size: 18px;
      margin: 0;
      font-weight: 650;
    }
    .status {
      display: flex;
      align-items: center;
      gap: 8px;
      color: var(--muted);
      font-size: 13px;
    }
    .dot {
      width: 9px;
      height: 9px;
      border-radius: 999px;
      background: var(--warn);
    }
    .dot.open { background: var(--accent); }
    main {
      height: calc(100vh - 56px);
      display: grid;
      grid-template-columns: minmax(280px, 34%) 1fr;
    }
    .list {
      min-width: 0;
      border-right: 1px solid var(--line);
      overflow: auto;
      background: var(--panel);
    }
    .empty {
      color: var(--muted);
      padding: 28px 18px;
      font-size: 14px;
    }
    .row {
      width: 100%;
      border: 0;
      border-bottom: 1px solid var(--line);
      background: transparent;
      color: var(--text);
      display: grid;
      grid-template-columns: 84px 1fr;
      gap: 10px;
      text-align: left;
      padding: 12px 14px;
      cursor: pointer;
      font: inherit;
    }
    .row:hover, .row.active { background: color-mix(in srgb, var(--accent) 10%, transparent); }
    .method {
      font-family: var(--mono);
      color: var(--accent);
      font-weight: 700;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .path {
      font-family: var(--mono);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      min-width: 0;
    }
    .meta {
      grid-column: 1 / -1;
      color: var(--muted);
      font-size: 12px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .detail {
      min-width: 0;
      overflow: auto;
      padding: 18px;
    }
    .detail h2 {
      margin: 0 0 12px;
      font-size: 20px;
      font-weight: 650;
      word-break: break-word;
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      gap: 10px;
      margin-bottom: 16px;
    }
    .field {
      border: 1px solid var(--line);
      background: var(--panel);
      border-radius: 6px;
      padding: 10px;
      min-width: 0;
    }
    .label {
      color: var(--muted);
      font-size: 12px;
      margin-bottom: 4px;
    }
    .value {
      font-family: var(--mono);
      font-size: 13px;
      overflow-wrap: anywhere;
    }
    pre {
      border: 1px solid var(--line);
      background: var(--panel);
      border-radius: 6px;
      padding: 14px;
      overflow: auto;
      font-family: var(--mono);
      font-size: 13px;
      line-height: 1.45;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
    .section-title {
      margin: 20px 0 8px;
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0;
      font-weight: 700;
    }
    @media (max-width: 760px) {
      main { grid-template-columns: 1fr; grid-template-rows: 42% 58%; }
      .list { border-right: 0; border-bottom: 1px solid var(--line); }
      header { padding: 0 12px; }
      .detail { padding: 14px; }
    }
  </style>
</head>
<body>
  <header>
		<h1>HTTP Sink Tap</h1>
    <div class="status"><span id="dot" class="dot"></span><span id="status">connecting</span></div>
  </header>
  <main>
    <section id="list" class="list">
      <div class="empty">No requests captured.</div>
    </section>
    <section id="detail" class="detail">
      <div class="empty">Waiting for requests.</div>
    </section>
  </main>
  <script>
    const list = document.getElementById("list");
    const detail = document.getElementById("detail");
    const statusText = document.getElementById("status");
    const dot = document.getElementById("dot");
    const requests = [];
    let selectedId = null;
    let socket;

    function connect() {
      const scheme = location.protocol === "https:" ? "wss" : "ws";
      socket = new WebSocket(scheme + "://" + location.host + "/ws");

      socket.addEventListener("open", () => {
        dot.classList.add("open");
        statusText.textContent = "connected";
      });

      socket.addEventListener("message", (event) => {
        const req = JSON.parse(event.data);
        const followNewest = shouldFollowNewest();
        requests.push(req);
        if (followNewest) {
          selectedId = req.id;
        }
        render({
          preserveDetailScroll: !followNewest,
          preserveListScroll: !followNewest,
          preservePrependedRows: !followNewest,
        });
      });

      socket.addEventListener("close", () => {
        dot.classList.remove("open");
        statusText.textContent = "reconnecting";
        setTimeout(connect, 1000);
      });

      socket.addEventListener("error", () => {
        socket.close();
      });
    }

    function shouldFollowNewest() {
      const newest = requests[requests.length - 1];
      return selectedId === null || (newest && selectedId === newest.id);
    }

    function render(options = {}) {
      const listScrollTop = list.scrollTop;
      const listScrollHeight = list.scrollHeight;
      const detailScrollTop = detail.scrollTop;

      renderList();
      if (options.preserveListScroll) {
        const prependedHeight = options.preservePrependedRows ? list.scrollHeight - listScrollHeight : 0;
        list.scrollTop = listScrollTop + prependedHeight;
      }

      const selected = requests.find((req) => req.id === selectedId) || requests[requests.length - 1];
      renderDetail(selected);
      if (options.preserveDetailScroll) {
        detail.scrollTop = detailScrollTop;
      }
    }

    function renderList() {
      list.textContent = "";
      if (requests.length === 0) {
        const empty = document.createElement("div");
        empty.className = "empty";
        empty.textContent = "No requests captured.";
        list.appendChild(empty);
        return;
      }

      requests.slice().reverse().forEach((req) => {
        const row = document.createElement("button");
        row.className = "row" + (req.id === selectedId ? " active" : "");
        row.type = "button";
        row.addEventListener("click", () => {
          const alreadySelected = selectedId === req.id;
          selectedId = req.id;
          render({
            preserveDetailScroll: alreadySelected,
            preserveListScroll: true,
          });
        });

        const method = document.createElement("div");
        method.className = "method";
        method.textContent = req.method;
        row.appendChild(method);

        const path = document.createElement("div");
        path.className = "path";
        path.textContent = req.request_uri || req.path;
        row.appendChild(path);

        const meta = document.createElement("div");
        meta.className = "meta";
        meta.textContent = "#" + req.id + "  " + new Date(req.received_at).toLocaleString() + "  " + req.remote_addr;
        row.appendChild(meta);

        list.appendChild(row);
      });
    }

    function renderDetail(req) {
      detail.textContent = "";
      if (!req) {
        const empty = document.createElement("div");
        empty.className = "empty";
        empty.textContent = "Waiting for requests.";
        detail.appendChild(empty);
        return;
      }

      const title = document.createElement("h2");
      title.textContent = req.method + " " + (req.request_uri || req.path);
      detail.appendChild(title);

      const grid = document.createElement("div");
      grid.className = "grid";
      addField(grid, "ID", "#" + req.id);
      addField(grid, "Received", new Date(req.received_at).toISOString());
      addField(grid, "Remote", req.remote_addr);
      addField(grid, "Host", req.host);
      addField(grid, "Protocol", req.proto);
      addField(grid, "Body", req.body.size + " bytes" + (req.body.truncated ? " truncated at " + req.body.limit : ""));
      detail.appendChild(grid);

      addPre("Headers", req.headers);
      addPre("Body", bodyValue(req.body));
      addPre("Full JSON", req);
    }

    function addField(parent, label, value) {
      const field = document.createElement("div");
      field.className = "field";
      const labelEl = document.createElement("div");
      labelEl.className = "label";
      labelEl.textContent = label;
      const valueEl = document.createElement("div");
      valueEl.className = "value";
      valueEl.textContent = value || "";
      field.appendChild(labelEl);
      field.appendChild(valueEl);
      parent.appendChild(field);
    }

    function addPre(title, value) {
      const heading = document.createElement("div");
      heading.className = "section-title";
      heading.textContent = title;
      const pre = document.createElement("pre");
      pre.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
      detail.appendChild(heading);
      detail.appendChild(pre);
    }

    function bodyValue(body) {
      if (!body) return "";
      if (body.encoding === "utf-8") return body.text || "";
      if (body.encoding === "base64") return body.base64 || "";
      if (body.read_error) return body.read_error;
      return "";
    }

    connect();
  </script>
</body>
</html>
`
