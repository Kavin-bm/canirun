// canirun local UI. Plain JS, no build step, no external requests.
"use strict";

const $ = (id) => document.getElementById(id);
const state = { hw: null, budget: null, est: null, measured: [], models: [] };

function el(tag, attrs = {}, ...kids) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") n.className = v;
    else if (k === "style") n.style.cssText = v; // CSSOM, allowed by the strict CSP
    else if (k.startsWith("on")) n.addEventListener(k.slice(2), v);
    else n.setAttribute(k, v);
  }
  for (const k of kids.flat()) if (k != null) n.append(k.nodeType ? k : String(k));
  return n;
}

async function api(path, opts) {
  const r = await fetch(path, opts);
  const body = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(body.error || r.statusText);
  return body;
}

const gb = (b) => (b / 2 ** 30).toFixed(1) + " GB";
const tps = (t) => (t <= 0 ? "–" : t < 10 ? t.toFixed(1) + " tok/s" : Math.round(t) + " tok/s");
const VERDICT = { fits: "fits", partial: "partial", no: "won't fit" };
const ICON = { fits: "✓", partial: "◐", no: "✕" };

function feel(t) {
  if (t >= 30) return "faster than you can read";
  if (t >= 10) return "comfortable reading speed";
  if (t >= 4) return "usable, but you will wait";
  if (t > 0) return "painfully slow";
  return "";
}

function headline(best) {
  if (!best) return ["no", "Won't fit"];
  if (best.verdict === "fits") return best.est_tokens_per_sec >= 10 ? ["fits", "Runs great"] : ["fits", "Fits, but slow"];
  return ["partial", best.est_tokens_per_sec >= 10 ? "Runs, partly on CPU" : "Runs, slowly"];
}

// ---------- theme ----------
function setTheme(t) {
  document.documentElement.dataset.theme = t;
  $("theme").textContent = t === "dark" ? "☀︎" : "☾";
  $("theme").setAttribute("aria-label", t === "dark" ? "Switch to light theme" : "Switch to dark theme");
  try { localStorage.setItem("canirun-theme", t); } catch (_) {}
  if (state.est) drawCard();
}
$("theme").addEventListener("click", () => setTheme(document.documentElement.dataset.theme === "dark" ? "light" : "dark"));
try { const t = localStorage.getItem("canirun-theme"); if (t) setTheme(t); } catch (_) {}

// ---------- hardware ----------
function renderHW() {
  const h = state.hw, b = state.budget;
  const chip = h.gpu || h.cpu || "this machine";
  $("chipName").textContent = chip;
  const rows = [
    ["Chip / GPU", chip + (h.gpu_cores ? ` (${h.gpu_cores} GPU cores)` : "")],
    ["CPU", `${h.cpu || "unknown"}, ${h.cores} cores`],
    ["RAM", gb(h.ram_bytes)],
    [b.kind === "unified" ? "GPU can use" : b.kind === "discrete" ? "VRAM" : "GPU", b.kind === "cpu" ? "none detected (CPU only)" : gb(b.gpu_bytes) + (b.kind === "unified" ? " of unified memory" : "")],
    ["Memory bandwidth", `${Math.round(h.gpu_bandwidth_gbps || h.cpu_bandwidth_gbps)} GB/s` + (h.bandwidth_source === "assumed" ? " (assumed)" : "")],
  ];
  $("hw").replaceChildren(...rows.map(([k, v]) => el("div", {}, el("dt", {}, k), el("dd", {}, v))));
}

// ---------- verdict ----------
function renderVerdict() {
  const e = state.est, best = e.best;
  const [cls, words] = headline(best);
  const m = e.model;
  const params = m.params_b + "B params" + (m.active_params_b ? `, ${m.active_params_b}B active (MoE)` : "");
  const head = el("div", { class: "vhead" },
    el("h3", {}, m.name),
    el("p", {}, `${params} · ${e.context.toLocaleString()} token context`),
    best
      ? el("p", { class: "big" }, `Use ${best.quant}: needs ~${gb(best.total_bytes)}, ~${tps(best.est_tokens_per_sec)}`)
      : el("p", { class: "big" }, "Not enough memory at any quantization. Try a smaller model or shorter context."),
    best ? el("p", {}, feel(best.est_tokens_per_sec)) : null,
    el("p", {}, el("span", { class: "tag est" }, "ESTIMATE"), " memory = weights + KV cache + runtime overhead"),
    ...(e.warnings || []).map((w) => el("p", { class: "warn" }, "⚠ " + w)),
  );
  const cap = Math.max(e.budget.total_bytes, ...e.results.map((r) => r.total_bytes));
  const quants = el("div", { class: "quants", role: "list", "aria-label": "Every quantization" },
    e.results.map((r) => {
      const isBest = best && r.quant === best.quant;
      let v = VERDICT[r.verdict];
      if (r.verdict === "partial") v += r.gpu_fraction > 0 ? ` · ${Math.round(r.gpu_fraction * 100)}% GPU` : " · CPU only";
      const bar = el("div", { class: "bar", "aria-hidden": "true" },
        el("i", { style: `width:${Math.min(100, (r.total_bytes / cap) * 100)}%` }),
        e.budget.gpu_bytes ? el("b", { style: `left:${(e.budget.gpu_bytes / cap) * 100}%`, title: "GPU memory limit" }) : null);
      return el("div", { class: `q ${r.verdict}${isBest ? " best" : ""}`, role: "listitem" },
        el("span", { class: "name" }, r.quant), bar,
        el("span", { class: "need" }, `${gb(r.total_bytes)} · ${tps(r.est_tokens_per_sec)}`),
        el("span", { class: "v" }, `${ICON[r.verdict]} ${v}`));
    }));
  $("verdict").replaceChildren(
    el("div", { class: `badge ${cls}`, role: "img", "aria-label": words }, el("div", {}, el("span", {}, ICON[cls]), words)),
    head, quants);
  $("verdict").hidden = false;
}

// ---------- speed chart ----------
function renderChart() {
  const e = state.est;
  const rows = e.results.filter((r) => r.est_tokens_per_sec > 0).map((r) => ({ kind: "est", label: `${r.quant} estimate`, v: r.est_tokens_per_sec }));
  for (const m of e.measured || []) rows.push({ kind: "meas", label: `${m.quant} · ${m.chip} (community)`, v: m.gen_tokens_per_sec });
  for (const m of state.measured.filter((x) => x.catalog_id === e.model.id))
    rows.push({ kind: "meas", label: `${m.quant} · this machine`, v: m.measured.gen_tokens_per_sec });
  const max = Math.max(1, ...rows.map((r) => r.v));
  const chart = $("chart");
  chart.setAttribute("aria-label", rows.map((r) => `${r.label}: ${tps(r.v)}`).join("; ") || "No speed data");
  chart.replaceChildren(...(rows.length ? rows.map((r) =>
    el("div", { class: `cbar ${r.kind}` },
      el("span", { class: "lbl", title: r.label }, el("span", { class: `tag ${r.kind}` }, r.kind === "est" ? "EST" : "MEAS"), " ", r.label),
      el("div", { class: "track" }, el("div", { class: "fill", style: `width:${(r.v / max) * 100}%` })),
      el("span", { class: "num" }, tps(r.v)))) : [el("p", { class: "muted" }, "Won't run here, so there is no speed to show.")]));
  $("speedCard").hidden = false;
}

// ---------- benchmark ----------
async function loadOllama() {
  const sel = $("ollamaModel");
  try {
    const o = await api("/api/ollama");
    const usable = o.models.filter((m) => !m.cloud && !m.embedding);
    if (!o.available) {
      sel.replaceChildren(el("option", {}, "Ollama not running"));
      $("benchNote").textContent = "Start Ollama (ollama serve) to measure real speed. canirun never downloads models.";
      return;
    }
    if (!usable.length) {
      sel.replaceChildren(el("option", {}, "No local models"));
      $("benchNote").textContent = "Pull a model yourself first, e.g. ollama pull llama3.2. Cloud models are skipped.";
      return;
    }
    sel.replaceChildren(...usable.map((m) => el("option", { value: m.name }, `${m.name} (${m.parameter_size || "?"}, ${m.quant || "?"}, ${gb(m.size_bytes)})`)));
    sel.disabled = false;
    $("benchBtn").disabled = false;
  } catch (err) {
    sel.replaceChildren(el("option", {}, "Ollama check failed"));
  }
}

$("benchBtn").addEventListener("click", () => { $("benchConfirm").hidden = false; $("benchGo").focus(); });
$("benchCancel").addEventListener("click", () => { $("benchConfirm").hidden = true; $("benchBtn").focus(); });
$("benchGo").addEventListener("click", async () => {
  $("benchConfirm").hidden = true;
  const model = $("ollamaModel").value;
  const status = $("benchStatus");
  $("benchBtn").disabled = true;
  status.textContent = `Measuring ${model}…`;
  try {
    const c = await api("/api/bench", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ model, ctx: Number($("ctx").value) }) });
    state.measured.push(c);
    const m = c.measured;
    status.textContent = `MEASURED ${m.model}: ${tps(m.gen_tokens_per_sec)} generation, ${Math.round(m.prompt_tokens_per_sec)} tok/s prompt` +
      (c.est_tokens_per_sec ? ` · estimate was ${tps(c.est_tokens_per_sec)} (${c.delta_pct > 0 ? "+" : ""}${c.delta_pct}%)` : c.note ? ` · ${c.note}` : "");
    if (c.catalog_id && (!state.est || state.est.model.id !== c.catalog_id)) {
      $("model").value = c.catalog_id;
      await check();
    } else if (state.est) { renderChart(); drawCard(); }
  } catch (err) {
    status.textContent = "Benchmark failed: " + err.message;
  } finally {
    $("benchBtn").disabled = false;
  }
});

// ---------- recommendations ----------
async function loadRecs() {
  const recs = await api("/api/recommend?ctx=" + $("ctx").value);
  const body = $("recs").tBodies[0];
  if (!recs.length) {
    body.replaceChildren(el("tr", {}, el("td", { colspan: 5, class: "muted" }, "Nothing in the catalog runs here at this context.")));
    return;
  }
  body.replaceChildren(...recs.slice(0, 12).map((r) => el("tr", {},
    el("td", {}, el("button", { class: "link", type: "button", onclick: () => { $("model").value = r.model.id; check(); } }, r.model.id)),
    el("td", {}, r.best.quant), el("td", {}, gb(r.best.total_bytes)), el("td", {}, tps(r.best.est_tokens_per_sec)),
    el("td", {}, `${ICON[r.best.verdict]} ${VERDICT[r.best.verdict]}`))));
}

// ---------- share card ----------
function cssVar(n) { return getComputedStyle(document.documentElement).getPropertyValue(n).trim(); }

function drawCard() {
  const e = state.est, best = e.best, h = state.hw;
  const c = $("card"), g = c.getContext("2d");
  const W = c.width, H = c.height;
  const [cls, words] = headline(best);
  const color = { fits: "#22c55e", partial: "#f5b83d", no: "#f2555a" }[cls];
  const sans = cssVar("--sans"), mono = cssVar("--mono");
  const grd = g.createLinearGradient(0, 0, W, H);
  grd.addColorStop(0, "#0b0d10"); grd.addColorStop(1, "#13202a");
  g.fillStyle = grd; g.fillRect(0, 0, W, H);
  g.fillStyle = color; g.globalAlpha = 0.12; g.beginPath(); g.arc(W - 120, 120, 300, 0, 7); g.fill(); g.globalAlpha = 1;

  g.fillStyle = "#8b97a5"; g.font = `600 26px ${sans}`;
  g.fillText(`Can my ${h.gpu || h.cpu} (${gb(h.ram_bytes)}) run`, 64, 96);
  g.fillStyle = "#e8edf2"; g.font = `800 64px ${sans}`;
  fitText(g, e.model.name + "?", 64, 172, W - 128);

  g.fillStyle = color; g.font = `800 92px ${sans}`;
  g.fillText(`${ICON[cls]} ${words}`, 64, 300);

  g.font = `600 30px ${sans}`; g.fillStyle = "#e8edf2";
  const mine = state.measured.filter((x) => x.catalog_id === e.model.id).pop();
  let line = best ? `${best.quant} · needs ${gb(best.total_bytes)} · ~${tps(best.est_tokens_per_sec)} estimated` : "Needs more memory than this machine has";
  g.fillText(line, 64, 370);
  if (mine) {
    g.fillStyle = "#22c55e";
    g.fillText(`${tps(mine.measured.gen_tokens_per_sec)} MEASURED with ${mine.measured.runtime}`, 64, 420);
  }

  g.font = `500 22px ${mono}`; g.fillStyle = "#8b97a5";
  g.fillText(`${e.context.toLocaleString()} token context · ${Math.round(h.gpu_bandwidth_gbps || h.cpu_bandwidth_gbps)} GB/s memory`, 64, 500);
  g.fillStyle = "#22c55e"; roundRect(g, 64, 540, 44, 44, 10); g.fill();
  g.fillStyle = "#052e16"; g.font = `900 28px ${sans}`; g.fillText("✓", 74, 572);
  g.fillStyle = "#e8edf2"; g.font = `700 28px ${mono}`; g.fillText("canirun", 124, 572);
  g.fillStyle = "#8b97a5"; g.font = `500 22px ${sans}`;
  g.textAlign = "right"; g.fillText("Check before you download", W - 64, 572); g.textAlign = "left";
  $("shareCard").hidden = false;
}

function fitText(g, text, x, y, maxW) {
  let size = parseInt(g.font.match(/(\d+)px/)[1], 10);
  while (g.measureText(text).width > maxW && size > 24) { size -= 2; g.font = g.font.replace(/\d+px/, size + "px"); }
  g.fillText(text, x, y);
}
function roundRect(g, x, y, w, h, r) { g.beginPath(); g.roundRect ? g.roundRect(x, y, w, h, r) : g.rect(x, y, w, h); }

function shareText() {
  const e = state.est, best = e.best, h = state.hw;
  const [, words] = headline(best);
  const mine = state.measured.filter((x) => x.catalog_id === e.model.id).pop();
  return `Can my ${h.gpu || h.cpu} (${gb(h.ram_bytes)}) run ${e.model.name}? ${words}.` +
    (best ? ` ${best.quant}, ~${tps(best.est_tokens_per_sec)} (estimate)` : "") +
    (mine ? `, ${tps(mine.measured.gen_tokens_per_sec)} measured` : "") + ". Checked with canirun.";
}

$("png").addEventListener("click", () => {
  $("card").toBlob((b) => {
    const a = el("a", { href: URL.createObjectURL(b), download: `canirun-${state.est.model.id}.png` });
    document.body.append(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
    $("shareStatus").textContent = "Saved PNG.";
  }, "image/png");
});
$("copy").addEventListener("click", async () => {
  try { await navigator.clipboard.writeText(shareText()); $("shareStatus").textContent = "Copied."; }
  catch (_) { $("shareStatus").textContent = shareText(); }
});

// ---------- flow ----------
async function check() {
  const name = $("model").value.trim();
  if (!name) return;
  try {
    state.est = await api(`/api/estimate?model=${encodeURIComponent(name)}&ctx=${$("ctx").value}`);
    renderVerdict(); renderChart(); drawCard();
    history.replaceState(null, "", `#${encodeURIComponent(name)}`);
  } catch (err) {
    $("verdict").replaceChildren(el("p", { class: "warn" }, err.message));
    $("verdict").hidden = false;
  }
}

$("pick").addEventListener("submit", (ev) => { ev.preventDefault(); check(); });
$("ctx").addEventListener("change", () => { loadRecs(); if ($("model").value) check(); });

(async function init() {
  const [hwr, models] = await Promise.all([api("/api/hw"), api("/api/models")]);
  state.hw = hwr.hardware; state.budget = hwr.budget; state.models = models;
  $("ver").textContent = "v" + hwr.version;
  renderHW();
  $("modelList").replaceChildren(...models.flatMap((m) => [m.id, ...(m.aliases || [])].map((v) => el("option", { value: v }, m.name))));
  loadRecs(); loadOllama();
  const fromHash = decodeURIComponent(location.hash.slice(1));
  $("model").value = fromHash || "qwen3:8b";
  check();
})().catch((err) => { $("hw").replaceChildren(el("div", {}, el("dt", {}, "Error"), el("dd", {}, err.message))); });
