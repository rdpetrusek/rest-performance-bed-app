const $ = (id) => document.getElementById(id);

async function fetchJSON(url, opts) {
  const r = await fetch(url, opts);
  if (!r.ok) throw new Error(`${url} -> ${r.status}`);
  return r.json();
}

function fmt(n, digits = 0) {
  if (n === null || n === undefined || Number.isNaN(n)) return "-";
  return Number(n).toFixed(digits);
}

async function refreshLive() {
  try {
    const live = await fetchJSON("/api/live");
    const device = live.device || {};
    const prefs = live.preferences || {};
    const body = live.body || {};
    const temp = live.temperature || {};

    $("device-name").textContent = device.name || "(connecting...)";
    $("device-meta").textContent = device.model
      ? `model ${device.model} - ${device.configuration || ""} - ${device.address || ""}`
      : "";

    $("c-present").textContent = body.present === undefined ? "-" : (body.present ? "Yes" : "No");
    $("c-heartrate").textContent = body.heartrate ? `${fmt(body.heartrate)} bpm` : "-";
    $("c-respiration").textContent = body.breathing && body.breathing.rate
      ? `${fmt(body.breathing.rate, 1)} /min`
      : "-";
    $("c-moving").textContent = body.moving === undefined ? "-" : (body.moving ? "Moving" : "Still");
    $("c-firmness").textContent = prefs.firmness !== undefined ? prefs.firmness : "-";
    $("c-cputemp").textContent = temp.cpu !== undefined ? `${fmt(temp.cpu, 1)} C` : "-";

    if (prefs.mode) $("mode-select").value = prefs.mode;
    if (prefs.firmness !== undefined) {
      $("firmness-slider").value = prefs.firmness;
      $("firmness-value").textContent = prefs.firmness;
    }
  } catch (e) {
    $("device-name").textContent = "(unreachable)";
    console.error(e);
  }
}

async function refreshSession() {
  try {
    const s = await fetchJSON("/api/analysis/last-session");
    const el = $("session-summary");
    if (!s.found) {
      el.textContent = s.reason || "No session found yet.";
      return;
    }
    const hr = s.heart_rate ? `${fmt(s.heart_rate.avg)} bpm avg (${fmt(s.heart_rate.min)}-${fmt(s.heart_rate.max)})` : "no data";
    const rr = s.respiration_rate ? `${fmt(s.respiration_rate.avg, 1)} /min avg (${fmt(s.respiration_rate.min, 1)}-${fmt(s.respiration_rate.max, 1)})` : "no data";
    el.innerHTML = `
      <strong>${s.ongoing ? "In progress" : "Completed"}</strong> -
      started ${new Date(s.start).toLocaleString()},
      duration ${fmt(s.duration_minutes)} min<br>
      Heart rate: ${hr}<br>
      Respiration: ${rr}<br>
      Movement events: ${s.movement_events} (${fmt((s.movement_ratio || 0) * 100, 1)}% of samples)
    `;
  } catch (e) {
    $("session-summary").textContent = "Error loading session summary.";
    console.error(e);
  }
}

let chart = null;

async function refreshHistory() {
  const hours = $("range-select").value;
  try {
    const [bodyRows, prefRows] = await Promise.all([
      fetchJSON(`/api/history?event=body&hours=${hours}`),
      fetchJSON(`/api/history?event=preferences&hours=${hours}`),
    ]);

    const hrPoints = bodyRows
      .filter((r) => r.data.heartrate)
      .map((r) => ({ x: r.ts, y: r.data.heartrate }));
    const rrPoints = bodyRows
      .filter((r) => r.data.breathing && r.data.breathing.rate)
      .map((r) => ({ x: r.ts, y: r.data.breathing.rate }));
    const firmnessPoints = prefRows
      .filter((r) => r.data.firmness !== undefined)
      .map((r) => ({ x: r.ts, y: r.data.firmness }));

    const ctx = $("chart-vitals").getContext("2d");
    const datasets = [
      { label: "Heart rate (bpm)", data: hrPoints, borderColor: "#5ec2a8", yAxisID: "y", tension: 0.2, pointRadius: 0 },
      { label: "Respiration (/min)", data: rrPoints, borderColor: "#6aa0d8", yAxisID: "y", tension: 0.2, pointRadius: 0 },
      { label: "Firmness", data: firmnessPoints, borderColor: "#d8a65e", yAxisID: "y1", tension: 0.2, pointRadius: 0 },
    ];

    if (chart) {
      chart.data.datasets = datasets;
      chart.update();
    } else {
      chart = new Chart(ctx, {
        type: "line",
        data: { datasets },
        options: {
          parsing: false,
          scales: {
            x: { type: "time", time: { unit: "hour" } },
            y: { position: "left" },
            y1: { position: "right", grid: { drawOnChartArea: false } },
          },
          interaction: { mode: "nearest", axis: "x", intersect: false },
        },
      });
    }
  } catch (e) {
    console.error(e);
  }
}

function setStatus(msg, isError = false) {
  const el = $("control-status");
  el.textContent = msg;
  el.style.color = isError ? "#d9534f" : "";
}

async function postControl(path, body) {
  try {
    await fetchJSON(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
    setStatus(`OK: ${path}`);
  } catch (e) {
    setStatus(`Failed: ${path} - ${e.message}`, true);
  }
}

$("apply-mode").onclick = () => postControl("/api/control/mode", { mode: $("mode-select").value });
$("apply-firmness").onclick = () => postControl("/api/control/firmness", { value: Number($("firmness-slider").value) });
$("firmness-slider").oninput = () => { $("firmness-value").textContent = $("firmness-slider").value; };
$("apply-profile").onclick = () => postControl("/api/control/manual-profile", {
  zones: [$("zone0"), $("zone1"), $("zone2"), $("zone3")].map((el) => Number(el.value)),
});
$("btn-fill").onclick = () => postControl("/api/control/fill");
$("btn-stop").onclick = () => postControl("/api/control/stop");
$("refresh-history").onclick = refreshHistory;
$("range-select").onchange = refreshHistory;

refreshLive();
refreshSession();
refreshHistory();
setInterval(refreshLive, 3000);
setInterval(refreshSession, 60000);
