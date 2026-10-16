// ============================================================
// VIRTUAL SHELL
// ============================================================
// Works only on the simulation's virtual filesystem (`files`,
// defined in mission.js). Never touches the user's real filesystem.
//
// Supported:
//   clear
//   ls [path]                     ls, ls /etc, ls /var/log/nginx
//   cat <file> [file...]          cat /mission.txt
//   grep [-E -i -v -c -n] <pattern> [file]
//   <cmd> | <cmd> | ...           cat /var/log/nginx/access.log | grep -E "500|502"
//   help
// ============================================================


// ------------------------------------------------------------
// HELPERS
// ------------------------------------------------------------

function normalizePath(path) {
    path = (path || "/").trim();
    if (!path.startsWith("/")) path = "/" + path;
    path = path.replace(/\/+/g, "/");
    if (path.length > 1 && path.endsWith("/")) path = path.slice(0, -1);
    return path;
}

// Split a string on `|` that is not inside quotes.
function splitPipes(input) {
    const parts = [];
    let cur = "", quote = null;

    for (const ch of input) {
        if (quote) {
            if (ch === quote) quote = null;
            cur += ch;
        } else if (ch === '"' || ch === "'") {
            quote = ch;
            cur += ch;
        } else if (ch === "|") {
            parts.push(cur);
            cur = "";
        } else {
            cur += ch;
        }
    }
    parts.push(cur);
    return parts.map(p => p.trim());
}

// Split a command into words, honoring "double" and 'single' quotes.
// Quotes may appear mid-word, e.g. cut -d' ' -f1
function tokenize(str) {
    const tokens = [];
    let cur = "", started = false, quote = null;

    for (const ch of str) {
        if (quote) {
            if (ch === quote) quote = null;
            else cur += ch;
        } else if (ch === '"' || ch === "'") {
            quote = ch;
            started = true;
        } else if (/\s/.test(ch)) {
            if (started) { tokens.push(cur); cur = ""; started = false; }
        } else {
            cur += ch;
            started = true;
        }
    }
    if (started) tokens.push(cur);
    return tokens;
}

function isDirectory(path) {
    if (path === "/") return true;
    const prefix = path + "/";
    return Object.keys(files).some(f => f.startsWith(prefix));
}


// ------------------------------------------------------------
// ls
// ------------------------------------------------------------

function listDirectory(path) {
    const prefix = path === "/" ? "/" : path + "/";
    const entries = new Set();

    for (const filePath of Object.keys(files)) {
        if (!filePath.startsWith(prefix)) continue;

        const remaining = filePath.slice(prefix.length);
        if (!remaining) continue;

        const parts = remaining.split("/");
        // Directories get a trailing slash.
        entries.add(parts.length > 1 ? parts[0] + "/" : parts[0]);
    }

    return [...entries].sort();
}

function commandLS(args) {
    const paths = args.filter(a => !a.startsWith("-"));
    if (paths.length === 0) paths.push("/");

    const out = [];

    for (const raw of paths) {
        const path = normalizePath(raw);

        if (path in files) {
            out.push(path);                       // ls on a file prints it
        } else if (isDirectory(path)) {
            const list = listDirectory(path).join("    ");
            out.push(paths.length > 1 ? `${path}:\n${list}` : list);
        } else {
            out.push(`ls: cannot access '${raw}': No such file or directory`);
        }
    }

    return out.join("\n\n");
}


// ------------------------------------------------------------
// cat
// ------------------------------------------------------------

function commandCAT(args, stdin) {
    const paths = args.filter(a => !a.startsWith("-"));

    if (paths.length === 0) {
        return stdin ?? "";                       // `... | cat`
    }

    return paths.map(raw => {
        const path = normalizePath(raw);

        if (path in files) return files[path];
        if (isDirectory(path)) return `cat: ${raw}: Is a directory`;
        return `cat: ${raw}: No such file or directory`;
    }).join("\n");
}


// ------------------------------------------------------------
// grep
// ------------------------------------------------------------

function commandGREP(args, stdin) {
    const flags = new Set();
    const positional = [];

    for (const a of args) {
        if (a.startsWith("-") && a.length > 1 && positional.length === 0) {
            for (const f of a.slice(1)) flags.add(f);
        } else {
            positional.push(a);
        }
    }

    const pattern = positional[0];
    const path = positional[1];

    if (pattern === undefined) {
        return "usage: grep [-E] [-i] [-v] [-c] [-n] pattern [file]";
    }

    let text;

    if (path !== undefined) {
        const p = normalizePath(path);
        if (!(p in files)) return `grep: ${path}: No such file or directory`;
        text = files[p];
    } else if (stdin !== null && stdin !== undefined) {
        text = stdin;
    } else {
        return "usage: grep [-E] [-i] [-v] [-c] [-n] pattern [file]";
    }

    let regex;
    try {
        regex = new RegExp(pattern, flags.has("i") ? "i" : "");
    } catch {
        return `grep: invalid pattern: ${pattern}`;
    }

    const invert = flags.has("v");

    let hits = text
        .split("\n")
        .map((line, i) => ({ line, n: i + 1 }))
        .filter(({ line }) => line.length && regex.test(line) !== invert);

    if (flags.has("c")) return String(hits.length);

    return hits
        .map(({ line, n }) => (flags.has("n") ? `${n}:${line}` : line))
        .join("\n");
}


// ------------------------------------------------------------
// head / tail / wc / cut / sort / uniq
// ------------------------------------------------------------

function textLines(text) {
    const lines = text.split("\n");
    if (lines[lines.length - 1] === "") lines.pop();
    return lines;
}

// Reads from the first file argument, or from piped stdin.
function readInput(paths, stdin, cmd) {
    if (paths.length) {
        const p = normalizePath(paths[0]);
        if (!(p in files)) return { err: `${cmd}: ${paths[0]}: No such file or directory` };
        return { text: files[p] };
    }
    if (stdin !== null && stdin !== undefined) return { text: stdin };
    return { err: `${cmd}: missing input (give a file or pipe something in)` };
}

function parseCount(args) {
    let n = 10;
    const rest = [];
    for (let i = 0; i < args.length; i++) {
        const a = args[i];
        if (a === "-n" && args[i + 1] !== undefined) n = parseInt(args[++i], 10);
        else if (/^-n\d+$/.test(a)) n = parseInt(a.slice(2), 10);
        else if (/^-\d+$/.test(a)) n = parseInt(a.slice(1), 10);
        else rest.push(a);
    }
    return { n: Number.isNaN(n) ? 10 : n, rest };
}

function commandHEAD(args, stdin) {
    const { n, rest } = parseCount(args);
    const { text, err } = readInput(rest, stdin, "head");
    return err ?? textLines(text).slice(0, n).join("\n");
}

function commandTAIL(args, stdin) {
    const { n, rest } = parseCount(args);
    const { text, err } = readInput(rest, stdin, "tail");
    if (err) return err;
    const lines = textLines(text);
    return (n === 0 ? [] : lines.slice(-n)).join("\n");
}

function commandWC(args, stdin) {
    const paths = args.filter(a => !a.startsWith("-"));
    const { text, err } = readInput(paths, stdin, "wc");
    if (err) return err;
    const count = textLines(text).length;
    return paths.length ? `${count} ${paths[0]}` : String(count);
}

function commandCUT(args, stdin) {
    let delim = "\t", fields = null;
    const paths = [];

    for (let i = 0; i < args.length; i++) {
        const a = args[i];
        if (a === "-d") delim = args[++i] ?? "";
        else if (a.startsWith("-d")) delim = a.slice(2);
        else if (a === "-f") fields = args[++i];
        else if (a.startsWith("-f")) fields = a.slice(2);
        else paths.push(a);
    }

    if (!fields) return "usage: cut -d<delim> -f<n[,n]> [file]";

    const idx = fields.split(",").map(n => parseInt(n, 10) - 1);
    if (idx.some(i => !(i >= 0))) return "cut: invalid field list";

    const { text, err } = readInput(paths, stdin, "cut");
    if (err) return err;

    return textLines(text).map(line => {
        const parts = line.split(delim);
        return idx.filter(i => i < parts.length).map(i => parts[i]).join(delim);
    }).join("\n");
}

function commandSORT(args, stdin) {
    const flags = new Set();
    const paths = [];
    for (const a of args) {
        if (a.startsWith("-") && a.length > 1) for (const f of a.slice(1)) flags.add(f);
        else paths.push(a);
    }

    const { text, err } = readInput(paths, stdin, "sort");
    if (err) return err;

    const num = l => parseFloat(l.trim()) || 0;
    const lines = textLines(text).sort(
        flags.has("n") ? (a, b) => num(a) - num(b) : (a, b) => (a < b ? -1 : a > b ? 1 : 0)
    );
    if (flags.has("r")) lines.reverse();
    return lines.join("\n");
}

function commandUNIQ(args, stdin) {
    const count = args.includes("-c");
    const paths = args.filter(a => !a.startsWith("-"));
    const { text, err } = readInput(paths, stdin, "uniq");
    if (err) return err;

    const out = [];
    for (const line of textLines(text)) {
        const last = out[out.length - 1];
        if (last && last.line === line) last.n++;
        else out.push({ line, n: 1 });
    }
    return out.map(o => (count ? `${String(o.n).padStart(7)} ${o.line}` : o.line)).join("\n");
}


// ------------------------------------------------------------
// help
// ------------------------------------------------------------

function commandHELP() {
    return [
        "Available commands:",
        "  ls [path]                       list a directory",
        "  cat <file>                      print a file",
        "  grep [-E -i -v -c -n] pat [file] search lines (file or piped input)",
        "  head/tail [-n N] [file]         first/last N lines (default 10)",
        "  wc [-l] [file]                  count lines",
        "  cut -d<c> -f<n> [file]          pick fields, e.g. cut -d' ' -f1",
        "  sort [-r -n] [file]             sort lines",
        "  uniq [-c] [file]                collapse repeated lines (-c counts)",
        "  clear                           clear the screen",
        "",
        "Pipes work:  cat " + ACCESS_LOG + " | cut -d' ' -f1 | sort | uniq -c | sort -rn",
    ].join("\n");
}


// ------------------------------------------------------------
// COMMAND RUNNER
// ------------------------------------------------------------
// Returns { out, clear }.  `stdin` is null when nothing is piped.

function runStage(stage, stdin) {
    const tokens = tokenize(stage);
    const name = tokens[0];
    const args = tokens.slice(1);

    switch (name) {
        case "ls":    return commandLS(args);
        case "cat":   return commandCAT(args, stdin);
        case "grep":  return commandGREP(args, stdin);
        case "head":  return commandHEAD(args, stdin);
        case "tail":  return commandTAIL(args, stdin);
        case "wc":    return commandWC(args, stdin);
        case "cut":   return commandCUT(args, stdin);
        case "sort":  return commandSORT(args, stdin);
        case "uniq":  return commandUNIQ(args, stdin);
        case "help":  return commandHELP();
        default:      return `command not found: ${name}`;
    }
}

function runCommand(command) {
    command = command.trim();

    if (!command) return { out: "" };
    if (command === "clear") return { out: "", clear: true };

    let data = null;

    for (const stage of splitPipes(command)) {
        if (!stage) return { out: "syntax error near unexpected token `|'" };
        data = runStage(stage, data);
    }

    return { out: data ?? "" };
}


// ------------------------------------------------------------
// TERMINAL INPUT (Enter to run, ↑/↓ for history)
// ------------------------------------------------------------

const cmdHistory = [];
let historyIndex = 0;

$("cmd").addEventListener("keydown", event => {
    const input = event.target;

    if (event.key === "ArrowUp") {
        if (cmdHistory.length && historyIndex > 0) {
            historyIndex--;
            input.value = cmdHistory[historyIndex];
        }
        event.preventDefault();
        return;
    }

    if (event.key === "ArrowDown") {
        if (historyIndex < cmdHistory.length - 1) {
            historyIndex++;
            input.value = cmdHistory[historyIndex];
        } else {
            historyIndex = cmdHistory.length;
            input.value = "";
        }
        event.preventDefault();
        return;
    }

    if (event.key !== "Enter") return;

    const command = input.value.trim();
    if (!command) return;

    cmdHistory.push(command);
    historyIndex = cmdHistory.length;

    const { out, clear } = runCommand(command);

    // Make sure the terminal tab is showing (no-op re-render if it already is).
    if (tab !== "__terminal__") show("__terminal__");

    if (clear) {
        clearTerminal();
    } else {
        const body = out === ""
            ? `<span class="dim">(no output)</span>`
            : escapeHTML(out);

        appendTerminal(`
            <div class="terminal-command">
                <span class="terminal-prompt">$</span>
                ${escapeHTML(command)}
            </div>

            <pre class="terminal-result">${body}</pre>
        `);
    }

    input.value = "";
});