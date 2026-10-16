const $ = id => document.getElementById(id);


// ============================================================
// LOAD SIMULATION
// ============================================================

// Only logged-in users may play (also gives us the token for /submit).
const session = AuthAPI.requireLogin();
if (!session) throw new Error("Login required.");

const simulationRaw = sessionStorage.getItem("simulationResponse");

if (!simulationRaw) {
    window.location.href = "singleplayer.html";
    throw new Error("No simulation found.");
}

const simulation = JSON.parse(simulationRaw);
const files = simulation.files || {};

// ============================================================
// SIMULATION METADATA
// ============================================================

$("seed").textContent = simulation.seed || "--------";


// ============================================================
// ACCESS LOG PARSER
// ============================================================

let rows = [];
const logFilters = { ip: "", status: "", method: "", path: "" };
let tab = "/var/log/nginx/access.log";
let tail = false;


function parseAccessLog(content) {

    const lines = content
        .split("\n")
        .map(line => line.trim())
        .filter(Boolean);

    return lines.map(line => {

        /*
         * Expected nginx format:
         *
         * IP - - [timestamp] "METHOD /path HTTP/1.1" STATUS BYTES
         */

        const match = line.match(
            /^(\S+)\s+-\s+-\s+\[([^\]]+)\]\s+"(\w+)\s+(.+?)\s+HTTP\/[\d.]+"\s+(\d+)\s+(\d+)/
        );

        if (!match) {
            return {
                t: "",
                ip: "",
                method: "",
                endpoint: line,
                status: "",
                bytes: "",
                raw: line
            };
        }

        return {
            ip: match[1],
            t: match[2],
            method: match[3],
            endpoint: match[4],
            status: Number(match[5]),
            bytes: Number(match[6]),
            raw: line
        };
    });
}


// ============================================================
// LOG TABLE
// ============================================================

function rowHTML(r, isNew = false) {

    const ok = r.status >= 200 && r.status < 400;
    const statusClass = ok ? "ok" : "bad";

    return `
        <tr class="${isNew ? "new" : ""}">
            <td class="ts">${escapeHTML(r.t)}</td>
            <td>${escapeHTML(r.ip)}</td>
            <td title="${escapeHTML(r.endpoint)}">
                ${escapeHTML(r.method)} ${escapeHTML(r.endpoint)}
            </td>
            <td class="${statusClass}">
                ${r.status}
            </td>
            <td class="${statusClass}">
                ${r.bytes} B
            </td>
            <td class="ua">
                -
            </td>
        </tr>
    `;
}


function escapeHTML(value) {
    return String(value)
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#039;");
}


function matches(r) {
    if (logFilters.ip && !String(r.ip).toLowerCase().includes(logFilters.ip.toLowerCase())) return false;
    if (logFilters.status && String(r.status) !== logFilters.status) return false;
    if (logFilters.method && r.method !== logFilters.method) return false;
    if (logFilters.path && !String(r.endpoint).toLowerCase().includes(logFilters.path.toLowerCase())) return false;
    return true;
}

function filteredRows() { return rows.filter(matches); }

function updateFilterSummary() {
    const active = Object.values(logFilters).filter(Boolean).length;
    $("filter-summary").textContent = active ? `${filteredRows().length.toLocaleString()} matching / ${rows.length.toLocaleString()}` : "All entries";
}

function setupLogFilters() {
    const fields = {
        ip: $("filter-ip"), status: $("filter-status"), method: $("filter-method"), path: $("filter-path")
    };
    Object.entries(fields).forEach(([key, field]) => {
        field.value = logFilters[key];
        field.oninput = field.onchange = () => { logFilters[key] = field.value.trim(); renderLog(); };
    });
    $("clear-filters").onclick = () => { Object.keys(logFilters).forEach(key => { logFilters[key] = ""; fields[key].value = ""; }); renderLog(); fields.ip.focus(); };
}


function renderLog() {

    $("view").innerHTML = `
        <table>
            <colgroup>
                <col data-column="timestamp" style="width:150px">
                <col data-column="remote-ip" style="width:140px">
                <col data-column="request">
                <col data-column="status" style="width:90px">
                <col data-column="bytes" style="width:90px">
                <col data-column="user-agent" style="width:150px">
            </colgroup>

            <thead>
                <tr>
                    <th>TIMESTAMP</th>
                    <th>REMOTE IP</th>
                    <th>HTTP REQUEST METHOD & PATH</th>
                    <th>STATUS</th>
                    <th>BYTES</th>
                    <th class="ua">USER AGENT</th>
                </tr>
            </thead>

            <tbody id="tb">
                ${filteredRows().map(r => rowHTML(r)).join("") || `<tr><td colspan="6" class="empty-log">No log entries match these filters.</td></tr>`}
            </tbody>
        </table>
    `;

    const view = $("view");

    setupColumnResizers();
    view.scrollTop = view.scrollHeight;
    


    updateStatus();
    updateFilterSummary();
    setupLogFilters();
}

// ============================================================
// RESIZABLE LOG COLUMNS
// ============================================================

function setupColumnResizers() {
    const table = $("view").querySelector("table");

    if (!table) return;

    const cols = table.querySelectorAll("colgroup col");
    const headers = table.querySelectorAll("thead th");

    headers.forEach((header, index) => {

        // Don't add twice
        if (header.querySelector(".col-resizer")) return;

        const resizer = document.createElement("span");

        resizer.className = "col-resizer";
        resizer.title = "Drag to resize";

        header.appendChild(resizer);

        resizer.addEventListener("mousedown", event => {

            event.preventDefault();

            const startX = event.clientX;
            const startWidth = header.getBoundingClientRect().width;

            // Minimum width for each column
            const minWidth =
                index === 2 ? 180 : 70;

            document.body.classList.add("resizing-columns");

            function onMouseMove(moveEvent) {

                const newWidth =
                    Math.max(
                        minWidth,
                        startWidth +
                        moveEvent.clientX -
                        startX
                    );

                cols[index].style.width =
                    `${newWidth}px`;

                cols[index].style.minWidth =
                    `${newWidth}px`;

                cols[index].style.maxWidth =
                    `${newWidth}px`;
            }

            function onMouseUp() {

                document.body.classList.remove(
                    "resizing-columns"
                );

                document.removeEventListener(
                    "mousemove",
                    onMouseMove
                );

                document.removeEventListener(
                    "mouseup",
                    onMouseUp
                );
            }

            document.addEventListener(
                "mousemove",
                onMouseMove
            );

            document.addEventListener(
                "mouseup",
                onMouseUp
            );
        });
    });
}


function updateStatus() {

    const count = filteredRows().length;

    $("st").textContent =
        `LINE: ${count.toLocaleString()} / ${rows.length.toLocaleString()} UTF-8`;
}


function buildFilesystem(paths) {
    const root = {
        type: "directory",
        name: "/",
        children: {}
    };

    for (const path of paths) {
        const parts = path.split("/").filter(Boolean);

        let current = root;

        parts.forEach((part, index) => {
            const isFile = index === parts.length - 1;

            if (!current.children[part]) {
                current.children[part] = {
                    type: isFile ? "file" : "directory",
                    name: part,
                    path: "/" + parts.slice(0, index + 1).join("/"),
                    children: isFile ? undefined : {}
                };
            }

            if (!isFile) {
                current = current.children[part];
            }
        });
    }

    return root;
}

const filesystem = buildFilesystem(Object.keys(files));


function renderFilesystem( node, container, level = 0 ) {

    const entries =
        Object.values(node.children);

    for (const entry of entries) {

        if (entry.type === "directory") {

            const dir =
                document.createElement("div");

            dir.className = "dir";

            dir.style.paddingLeft =
                `${16 + level * 12}px`;

            dir.textContent =
                `▾ ${entry.name}/`;

            container.appendChild(dir);

            renderFilesystem(
                entry,
                container,
                level + 1
            );

        } else {

            const button =
                document.createElement("button");

            button.className = "f";

            button.dataset.path =
                entry.path;

            button.style.paddingLeft =
                `${24 + level * 12}px`;

            const icon =
                entry.path ===
                "/var/log/nginx/error.log"
                    ? "△"
                    : "▤";

            button.innerHTML = `
                <span>${icon}</span>
                <span>${entry.name}</span>
                <span>--</span>
            `;

            container.appendChild(button);
        }
    }
}


const filesystemContainer =
    document.getElementById("filesystem");

renderFilesystem(
    filesystem,
    filesystemContainer
);


// ============================================================
// DOCUMENTS
// ============================================================

function showDocument(path) {

    const content = files[path];

    $("view").innerHTML = `
        <pre class="doc">${escapeHTML(content || "")}</pre>
    `;

    $("st").textContent =
        `${path} · read-only`;
}

// ============================================================
// TABS
// ============================================================

const openTabs = [
    ...Object.keys(files),
    "__terminal__"
];

function renderTabs() {

    $("tabs").innerHTML =
        openTabs.map(path => {

            const name =
                path === "__terminal__"
                    ? "TERMINAL"
                    : path.split("/")
                        .filter(Boolean)
                        .pop();

            return `
                <button
                    class="tab${path === tab ? " on" : ""}"
                    data-path="${escapeHTML(path)}">
                    ${name}
                </button>
            `;

        }).join("") +
        `<span class="st" id="st"></span>`;
}
function show(path) {

    tab = path;

    renderTabs();

    document.querySelectorAll(".f").forEach(b =>
        b.classList.toggle(
            "on",
            b.dataset.path === path
        )
    );

    $("log-filters").hidden = path !== "/var/log/nginx/access.log";

    if (path === "__terminal__") {

        renderTerminal();

    } else if (path === "/var/log/nginx/access.log") {

        renderLog();

    } else {

        showDocument(path);
    }
}

// ============================================================
// FILE TREE
// ============================================================

document.addEventListener("click", event => {

    const button = event.target.closest("[data-path]");

    if (!button) {
        return;
    }

    show(button.dataset.path);
});



// ============================================================
// TAIL BUTTON
// ============================================================

$("tail").onclick = () => {

    tail = !tail;

    $("tail").setAttribute(
        "aria-pressed",
        tail
    );

    $("tail").style.color =
        tail ? "" : "var(--dim)";
};


// ============================================================
// ABORT
// ============================================================

$("abort").onclick = () => {

    tail = false;

    document.querySelector(".dot").style.background =
        "var(--bad)";

    $("tail").setAttribute(
        "aria-pressed",
        "false"
    );

    clearInterval(clockTimer);
};


// ============================================================
// SUBMIT
// ============================================================

// Must match the anomaly types the API generates.
const ANOMALY_TYPES = [
    ["credential_brute_force", "Credential brute force"],
    ["endpoint_scanning", "Endpoint scanning"],
    ["path_traversal", "Path traversal"]
];

// Number of objectives listed in /mission.txt (the most attacks a player can report).
const FINDING_COUNT = Math.max(
    1,
    ((files["/mission.txt"] || "").match(/^MISSION\s*$/gm) || []).length
);

// ------------------------------------------------------------
// The form works in two steps: pick a number of attackers, then
// fill one card per attacker (an IP plus the attacks they ran).
// The number options never change with the simulation, so the
// form gives nothing away about how many attackers there are.
// ------------------------------------------------------------

const submitState = {
    count: 0,          // 0 = not chosen yet
    attackers: [],     // [{ ip, attacks: [{ endpoint, type }] }]
    marks: {},         // "attacker:attack" -> API result for that finding
    accepted: false
};

const newAttacker = () => ({
    ip: "",
    attacks: [{ endpoint: "", type: "" }]
});

function setAttackerCount(count) {

    const list = submitState.attackers;

    if (count < list.length) {

        // Fewer attackers: the extra cards (and their attacks) are removed.
        list.splice(count);

    } else {

        while (list.length < count) {
            list.push(newAttacker());
        }
    }

    submitState.count = count;
    submitState.marks = {};

    renderAttackers();
}

function typeOptions(selected) {
    return `<option value="">Select a type</option>` +
        ANOMALY_TYPES.map(([value, label]) =>
            `<option value="${value}"${value === selected ? " selected" : ""}>${label}</option>`
        ).join("");
}

function countReported() {
    let n = 0;
    submitState.attackers.forEach(a =>
        a.attacks.forEach(x => { if (x.endpoint.trim() && x.type) n++; })
    );
    return n;
}

function updateSummary() {

    $("d-sum").textContent = submitState.count
        ? `${countReported()} attack${countReported() === 1 ? "" : "s"} reported`
        : "Choose the number of attackers to start.";

    $("d-send").disabled = !submitState.count || submitState.accepted;
}

function renderAttackers() {

    document.querySelectorAll("#seg [data-count]").forEach(button =>
        button.setAttribute(
            "aria-pressed",
            String(Number(button.dataset.count) === submitState.count)
        )
    );

    $("attacker-cards").innerHTML = submitState.attackers.map((attacker, ai) => `
        <div class="atk">
            <div class="atk-title">Attacker ${ai + 1}</div>

            <label for="ip-${ai}">Source IP</label>
            <input id="ip-${ai}" data-field="ip" data-a="${ai}"
                   value="${escapeHTML(attacker.ip)}"
                   placeholder="e.g. 10.20.30.40"
                   spellcheck="false" autocomplete="off">

            ${attacker.attacks.map((attack, ri) => {

                const mark = submitState.marks[ai + ":" + ri];

                return `
                <div class="atk-row">
                    <label for="ep-${ai}-${ri}">Endpoint</label>
                    <input id="ep-${ai}-${ri}" data-field="endpoint"
                           data-a="${ai}" data-r="${ri}"
                           value="${escapeHTML(attack.endpoint)}"
                           placeholder="e.g. /login"
                           spellcheck="false" autocomplete="off">

                    <label for="ty-${ai}-${ri}">Anomaly type</label>
                    <select id="ty-${ai}-${ri}" data-field="type"
                            data-a="${ai}" data-r="${ri}">
                        ${typeOptions(attack.type)}
                    </select>

                    ${attacker.attacks.length > 1
                        ? `<button type="button" class="btn" data-remove="${ai}:${ri}">Remove this attack</button>`
                        : ""}

                    ${mark ? `
                    <div class="atk-res">
                        <span class="${mark.source_ip ? "ok" : "bad"}">${mark.source_ip ? "&#10003;" : "&#10007;"} source IP</span>
                        <span class="${mark.endpoint ? "ok" : "bad"}">${mark.endpoint ? "&#10003;" : "&#10007;"} endpoint</span>
                        <span class="${mark.anomaly_type ? "ok" : "bad"}">${mark.anomaly_type ? "&#10003;" : "&#10007;"} anomaly type</span>
                    </div>` : ""}
                </div>`;
            }).join("")}

            <button type="button" class="btn" data-add="${ai}">+ Add another attack by this attacker</button>
        </div>
    `).join("");

    updateSummary();
}

$("seg").addEventListener("click", event => {

    const button = event.target.closest("[data-count]");

    if (button && !submitState.accepted) {
        setAttackerCount(Number(button.dataset.count));
    }
});

$("attacker-cards").addEventListener("input", event => {

    const field = event.target.dataset.field;

    if (!field) return;

    const ai = Number(event.target.dataset.a);
    const ri = Number(event.target.dataset.r);

    if (field === "ip") {

        submitState.attackers[ai].ip = event.target.value;

        // The IP applies to every attack of this attacker, so its old results are stale.
        Object.keys(submitState.marks)
            .filter(key => key.startsWith(ai + ":"))
            .forEach(key => delete submitState.marks[key]);

        event.target.closest(".atk")
            .querySelectorAll(".atk-res").forEach(el => el.remove());

    } else {

        const attack = submitState.attackers[ai].attacks[ri];

        if (field === "endpoint") attack.endpoint = event.target.value;
        if (field === "type") attack.type = event.target.value;

        delete submitState.marks[ai + ":" + ri];

        const row = event.target.closest(".atk-row");
        if (row) row.querySelectorAll(".atk-res").forEach(el => el.remove());
    }

    updateSummary();
});

$("attacker-cards").addEventListener("click", event => {

    const add = event.target.closest("[data-add]");
    const remove = event.target.closest("[data-remove]");

    if (add) {
        submitState.attackers[Number(add.dataset.add)]
            .attacks.push({ endpoint: "", type: "" });
        submitState.marks = {};
        renderAttackers();
    }

    if (remove) {
        const [ai, ri] = remove.dataset.remove.split(":").map(Number);
        submitState.attackers[ai].attacks.splice(ri, 1);
        submitState.marks = {};
        renderAttackers();
    }
});

// Turns the cards into the API's flat findings list: one entry per
// attack, repeating the attacker's IP. Empty attacks are skipped;
// half-filled ones are an error.
function readFindings() {

    const findings = [];
    const where = [];

    for (let ai = 0; ai < submitState.attackers.length; ai++) {

        const attacker = submitState.attackers[ai];
        const ip = attacker.ip.trim();

        for (let ri = 0; ri < attacker.attacks.length; ri++) {

            const endpoint = attacker.attacks[ri].endpoint.trim();
            const type = attacker.attacks[ri].type;

            if (!endpoint && !type) continue;

            if (!endpoint || !type) {
                return {
                    error: `Attacker ${ai + 1}: give both an endpoint and a type for each attack, or leave it empty.`
                };
            }

            if (!ip) {
                return { error: `Attacker ${ai + 1}: enter the source IP.` };
            }

            findings.push({
                source_ip: ip,
                endpoint: endpoint,
                anomaly_type: type
            });

            where.push(ai + ":" + ri);
        }
    }

    if (findings.length === 0) {
        return { error: "Report at least one attack." };
    }

    if (findings.length > FINDING_COUNT) {
        return {
            error: `The mission lists ${FINDING_COUNT} objective${FINDING_COUNT === 1 ? "" : "s"}, so you can report at most ${FINDING_COUNT} attack${FINDING_COUNT === 1 ? "" : "s"}.`
        };
    }

    return { findings, where };
}

function renderResultAnalysis(data) {
    const analysis = $("dossier-analysis");
    const matched = Number(data.matched) || 0;
    const total = Number(data.total) || 0;
    const score = Number(data.score) || 0;
    const accuracy = total ? Math.round((matched / total) * 100) : 0;
    const seconds = Math.max(0, Math.floor((Date.now() - startedAt) / 1000));
    const elapsed = `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;

    $("analysis-verdict").textContent = data.correct ? "MISSION COMPLETE" : "REVIEW NEEDED";
    $("analysis-verdict").className = data.correct ? "ok" : "bad";
    $("analysis-metrics").innerHTML = [
        [score + "/100", "Score"],
        [matched + "/" + total, "Objectives matched"],
        [accuracy + "%", "Accuracy"],
        [elapsed, "Investigation time"]
    ].map(metric => `<div class="analysis-metric"><b>${escapeHTML(metric[0])}</b><span>${escapeHTML(metric[1])}</span></div>`).join("");

    const findings = Array.isArray(data.findings) ? data.findings : [];
    $("analysis-breakdown").innerHTML = findings.length ? findings.map((finding, index) => {
        const complete = finding.correct === true;
        const fields = [
            ["IP", finding.source_ip],
            ["Path", finding.endpoint],
            ["Type", finding.anomaly_type]
        ].map(field => `${field[0]} ${field[1] ? "✓" : "✕"}`).join("  ·  ");
        return `<div class="analysis-row ${complete ? "good" : "bad"}"><b>Finding ${index + 1}</b><span>${fields}</span></div>`;
    }).join("") : `<div class="analysis-row bad"><b>No field-level breakdown</b><span>Review the mission objectives.</span></div>`;

    $("analysis-tip").textContent = data.correct
        ? "Excellent work. All reported objectives matched the simulated incident."
        : `${Math.max(0, total - matched)} objective${total - matched === 1 ? "" : "s"} still need review. Use the field breakdown above to refine your next submission.`;
    analysis.hidden = false;
}

$("submit").onclick = () => {
    $("dossier-result").textContent = "";
    $("dossier-analysis").hidden = true;
    renderAttackers();
    $("dossier").showModal();
};

$("d-cancel").onclick = () => $("dossier").close();

$("d-send").onclick = async () => {

    const result = $("dossier-result");

    const form = readFindings();

    if (form.error) {
        result.innerHTML = `<span class="bad">${escapeHTML(form.error)}</span>`;
        return;
    }

    const body = {
        seed: simulation.seed,
        environment: simulation.environment,
        difficulty: simulation.difficulty,
        findings: form.findings
    };

    $("d-send").disabled = true;
    result.textContent = "Sending...";

    // A classroom game (started from classroom.html) is submitted to its room,
    // so the score and time taken show up for the faculty.
    // A multiplayer game (started from room.html) is submitted to its room,
    // so the score and finish time show up on the scoreboard.
    const submitUrl = simulation.room_code
        ? `${API.rooms}/${encodeURIComponent(simulation.room_code)}/submit`
        : simulation.classroom_id
            ? `${API.classrooms}/${encodeURIComponent(simulation.classroom_id)}/submit`
            : API.singleplayerSubmit;

    try {
        const response = await fetch(submitUrl, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                "Authorization": "Bearer " + session.access_token
            },
            body: JSON.stringify(body)
        });

        // Session expired or revoked: log in again (this page is remembered).
        if (response.status === 401) {
            AuthAPI.clearSession();
            AuthAPI.requireLogin();
            return;
        }

        const data = await response.json();

        if (!response.ok) {
            throw new Error(data.error || "Submission failed");
        }

        submitState.marks = {};
        (data.findings || []).forEach((finding, k) => {
            submitState.marks[form.where[k]] = finding;
        });

        renderAttackers();

        result.innerHTML =
            `<b>SCORE ${data.score}/100 - ${data.correct ? "CORRECT" : "INCORRECT"}</b>` +
            ` &middot; ${data.matched}/${data.total} anomalies found`;
        renderResultAnalysis(data);

        if (data.correct) {
            submitState.accepted = true;
            $("submit").textContent = "Dossier accepted";
            $("submit").disabled = true;
        }

        // Show the feedback form after the mission result is saved.
        $("d-after").hidden = false;

    } catch (error) {
        result.innerHTML = `<span class="bad">${escapeHTML(error.message)}</span>`;
    } finally {
        updateSummary();
    }
};

$("mission-feedback").addEventListener("submit", event => {
    event.preventDefault();
    const rating = $("feedback-rating").value;
    const comment = $("feedback-comment").value.trim();
    const status = $("feedback-status");
    if (!rating) {
        status.className = "feedback-status bad";
        status.textContent = "Please select a rating.";
        return;
    }
    const scoreText = $("dossier-result").textContent.match(/SCORE\s+(\d+)/i);
    const payload = { rating: Number(rating), difficulty: simulation.difficulty || "", seed: simulation.seed || "", score: scoreText ? Number(scoreText[1]) : null, comment: comment };
    const button = event.target.querySelector("button[type=submit]");
    button.disabled = true;
    status.textContent = "Saving feedback...";
    AuthAPI.submitFeedback(payload, session.access_token).then(response => {
        if (response.status === 401) { AuthAPI.clearSession(); AuthAPI.requireLogin(); return; }
        if (!response.ok) throw new Error(response.data.error || "Could not save feedback.");
        status.className = "feedback-status ok";
        status.textContent = "Feedback saved and sent to the admin panel.";
        event.target.reset();
    }).catch(error => {
        button.disabled = false;
        status.className = "feedback-status bad";
        status.textContent = error.message;
    });
});


function renderTerminal() {

    $("view").innerHTML = `
        <div class="terminal-panel">

            <div class="terminal-output" id="terminal-output">
                <div class="terminal-line dim">
                    ChaosLab virtual shell
                </div>

                <div class="terminal-line dim">
                    Type <b>help</b> for available commands.
                </div>
            </div>

        </div>
    `;

    $("st").textContent = "virtual shell";
}

// ============================================================
// TELEMETRY (derived from the actual log, not hard-coded)
// ============================================================

function updateTelemetry() {

    const total = rows.length || 1;
    const errors = rows.filter(r => r.status >= 400).length;
    const authFailures = rows.filter(r => r.status === 401 || r.status === 403).length;

    $("err").textContent = (errors / total * 100).toFixed(1) + "%";
    $("gw").textContent = String(authFailures);
    $("filt").textContent = "[NONE]";

    $("note-text").textContent =
        `${rows.length.toLocaleString()} requests captured, ` +
        `${errors.toLocaleString()} errors, ` +
        `${authFailures.toLocaleString()} auth failures. ` +
        `Review the logs and identify the anomalies listed in the mission.`;
}

const startedAt = Date.now();

const clockTimer = setInterval(() => {
    const s = Math.floor((Date.now() - startedAt) / 1000);
    $("clock").textContent =
        String(Math.floor(s / 60)).padStart(2, "0") + ":" +
        String(s % 60).padStart(2, "0");
}, 1000);


// ============================================================
// INITIALIZE
// ============================================================

const ACCESS_LOG = "/var/log/nginx/access.log";

rows = parseAccessLog(
    files[ACCESS_LOG] || ""
);

updateTelemetry();
show("/mission.txt");
