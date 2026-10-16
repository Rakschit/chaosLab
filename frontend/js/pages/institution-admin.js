(function () {
  "use strict";

  var session = AuthAPI.requireLogin();
  if (!session) return;

  var csvInput = document.getElementById("csv");
  var preview = document.getElementById("preview");
  var importButton = document.getElementById("import");
  var status = document.getElementById("status");
  var importStatus = document.getElementById("import-status");
  var parsedRows = [];

  function message(el, text, kind) {
    el.textContent = text;
    el.className = "msg" + (kind ? " " + kind : "");
  }

  function parseCSV(text) {
    var rows = [];
    var row = [];
    var cell = "";
    var quoted = false;

    for (var i = 0; i < text.length; i++) {
      var ch = text.charAt(i);

      if (quoted) {
        if (ch === '"' && text.charAt(i + 1) === '"') {
          cell += '"';
          i++;
        } else if (ch === '"') {
          quoted = false;
        } else {
          cell += ch;
        }
      } else if (ch === '"') {
        quoted = true;
      } else if (ch === ",") {
        row.push(cell);
        cell = "";
      } else if (ch === "\n" || ch === "\r") {
        if (ch === "\r" && text.charAt(i + 1) === "\n") i++;
        row.push(cell);
        cell = "";

        if (row.some(function (v) { return v.trim() !== ""; })) {
          rows.push(row);
        }
        row = [];
      } else {
        cell += ch;
      }
    }

    if (quoted) {
      throw new Error("CSV has an unclosed quoted field.");
    }

    row.push(cell);
    if (row.some(function (v) { return v.trim() !== ""; })) {
      rows.push(row);
    }

    if (rows.length < 2) {
      throw new Error("CSV needs a header and at least one data row.");
    }

    var headers = rows.shift().map(function (h) {
      return h.replace(/^\uFEFF/, "").trim().toLowerCase();
    });

    ["email", "name", "role", "external_id"].forEach(function (h) {
      if (headers.indexOf(h) < 0) {
        throw new Error("Missing required header: " + h);
      }
    });

    if (headers.some(function (h) { return !h; })) {
      throw new Error("CSV contains an empty column name.");
    }

    if (headers.length !== new Set(headers).size) {
      throw new Error("CSV contains duplicate column names.");
    }

    if (rows.length > 10000) {
      throw new Error("CSV may contain at most 10,000 records.");
    }

    var seen = Object.create(null);

    return rows.map(function (values, idx) {
      if (values.length > headers.length) {
        throw new Error("Too many columns on data row " + (idx + 2) + ".");
      }

      var obj = {};

      headers.forEach(function (h, j) {
        obj[h] = (values[j] || "").trim();
      });

      obj.role = (obj.role || "").toLowerCase();

      if (["student", "faculty"].indexOf(obj.role) < 0) {
        throw new Error(
          "Invalid role on data row " + (idx + 2) +
          ". Use student or faculty."
        );
      }

      if (!obj.email && !obj.external_id) {
        throw new Error(
          "Row " + (idx + 2) + " needs an email or external_id."
        );
      }

      if (obj.email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(obj.email)) {
        throw new Error("Invalid email on data row " + (idx + 2) + ".");
      }

      if (obj.email) {
        obj.email = obj.email.toLowerCase();
      }

      if (obj.external_id && obj.external_id.length > 128) {
        throw new Error("external_id is too long on row " + (idx + 2) + ".");
      }

      var identity = obj.external_id
        ? "id:" + obj.external_id
        : "email:" + obj.email;

      if (seen[identity]) {
        throw new Error("Duplicate identity on data row " + (idx + 2) + ".");
      }
      seen[identity] = true;

      return {
        email: obj.email,
        name: obj.name,
        role: obj.role,
        external_id: obj.external_id
      };
    });
  }

  function handleUnauthorized(res) {
    if (res.status !== 401) return false;

    AuthAPI.clearSession();
    window.location.replace("login.html");
    return true;
  }

  function loadStatus() {
    AuthAPI.authedGet(
      "/institution/roster",
      session.access_token
    ).then(function (res) {
      if (!res.ok) {
        if (handleUnauthorized(res)) return;

        message(
          status,
          res.data.error || "Could not load roster status.",
          "err"
        );
        return;
      }

      document.getElementById("students").textContent =
        String(res.data.students == null ? 0 : res.data.students);

      document.getElementById("faculty").textContent =
        String(res.data.faculty == null ? 0 : res.data.faculty);

      document.getElementById("enforcement").textContent =
        res.data.roster_enforced ? "ON" : "OFF";

      message(
        status,
        res.data.roster_enforced
          ? "Roster enforcement is active."
          : "No roster has been imported yet; access is not roster-restricted.",
        res.data.roster_enforced ? "good" : ""
      );
    }).catch(function () {
      message(status, "Could not reach the API.", "err");
    });
  }

  csvInput.addEventListener("change", function () {
    parsedRows = [];
    importButton.disabled = true;
    message(importStatus, "");

    var file = csvInput.files && csvInput.files[0];

    if (!file) {
      preview.textContent = "Choose a CSV to preview it before importing.";
      return;
    }

    if (file.size > 4 * 1024 * 1024) {
      preview.textContent = "File is larger than 4 MB.";
      return;
    }

    if (!/\.csv$/i.test(file.name)) {
      preview.textContent = "Please select a CSV file.";
      return;
    }

    var reader = new FileReader();

    reader.onload = function () {
      try {
        parsedRows = parseCSV(String(reader.result || ""));

        var students = parsedRows.filter(function (r) {
          return r.role === "student";
        }).length;

        var faculty = parsedRows.length - students;

        preview.textContent =
          "Ready to import " + parsedRows.length + " records\n" +
          "Students: " + students + "\n" +
          "Faculty: " + faculty + "\n\n" +
          "First five rows:\n" +
          parsedRows.slice(0, 5).map(function (r) {
            return [
              r.email || "(no email)",
              r.role,
              r.external_id || "(no external_id)"
            ].join(" | ");
          }).join("\n");

        importButton.disabled = false;
      } catch (e) {
        preview.textContent = e.message;
        parsedRows = [];
        importButton.disabled = true;
      }
    };

    reader.onerror = function () {
      preview.textContent = "Could not read the CSV file.";
    };

    reader.readAsText(file, "UTF-8");
  });

  importButton.addEventListener("click", function () {
    if (!parsedRows.length) return;

    var confirmed = window.confirm(
      "Import this roster? If this is a full roster sync, " +
      "people missing from the CSV may lose access. Continue only " +
      "after reviewing the file."
    );

    if (!confirmed) return;

    importButton.disabled = true;
    message(importStatus, "Importing roster…");

    AuthAPI.authedPost(
      "/institution/roster",
      { rows: parsedRows },
      session.access_token
    ).then(function (res) {
      if (res.ok) {
        message(
          importStatus,
          res.data.message || "Roster imported successfully.",
          "good"
        );
        parsedRows = [];
        csvInput.value = "";
        preview.textContent = "Roster imported. Choose a CSV to import again.";
        loadStatus();
        return;
      }

      if (handleUnauthorized(res)) return;

      message(
        importStatus,
        res.data.error || "Import failed. Check the server response.",
        "err"
      );
    }).catch(function () {
      message(importStatus, "Could not reach the API.", "err");
    }).then(function () {
      importButton.disabled = parsedRows.length === 0;
    });
  });

  document.getElementById("logout").addEventListener("click", function (e) {
    e.preventDefault();

    AuthAPI.logout(session.access_token).then(function () {
      AuthAPI.clearSession();
      window.location.replace("login.html");
    }, function () {
      AuthAPI.clearSession();
      window.location.replace("login.html");
    });
  });

  // ---------------------------------------------------------------------
  // Access requests
  // ---------------------------------------------------------------------
  var requestsBox = document.getElementById("requests");
  var requestsStatus = document.getElementById("requests-status");

  function el(tag, cls, text) {
    var node = document.createElement(tag);
    if (cls) node.className = cls;
    if (text != null) node.textContent = text;
    return node;
  }

  function renderRequests(list) {
    requestsBox.textContent = "";

    if (!list.length) {
      message(requestsStatus, "No pending requests.");
      return;
    }

    message(requestsStatus, list.length + " pending.");

    list.forEach(function (r) {
      var card = el("div", "req");
      card.appendChild(el("div", "who", r.display_name || r.email));
      card.appendChild(el(
        "div", "meta",
        r.email + " · asked to join as " + r.role + " · " +
        new Date(r.created_at).toLocaleDateString()
      ));
      if (r.message) card.appendChild(el("div", "text", r.message));

      var note = document.createElement("textarea");
      note.maxLength = 500;
      note.placeholder = "Note to the person (optional)";
      card.appendChild(note);

      var ctl = el("div", "ctl");
      var role = document.createElement("select");
      ["student", "faculty"].forEach(function (name) {
        var opt = document.createElement("option");
        opt.value = name;
        opt.textContent = "Add as " + name;
        if (name === r.role) opt.selected = true;
        role.appendChild(opt);
      });
      var approve = el("button", "", "Approve");
      approve.type = "button";
      var reject = el("button", "no", "Reject");
      reject.type = "button";
      ctl.appendChild(role);
      ctl.appendChild(approve);
      ctl.appendChild(reject);
      card.appendChild(ctl);

      function decide(decision) {
        approve.disabled = true;
        reject.disabled = true;
        message(requestsStatus, "Saving…");

        AuthAPI.authedPost("/institution/access-requests/decision", {
          id: r.id,
          decision: decision,
          role: role.value,
          note: note.value.trim()
        }, session.access_token).then(function (res) {
          if (res.ok) {
            loadRequests(res.data.message);
            loadStatus();
            return;
          }
          if (handleUnauthorized(res)) return;
          message(requestsStatus, res.data.error || "Could not save the decision.", "err");
          approve.disabled = false;
          reject.disabled = false;
        }).catch(function () {
          message(requestsStatus, "Could not reach the API.", "err");
          approve.disabled = false;
          reject.disabled = false;
        });
      }

      approve.addEventListener("click", function () { decide("approve"); });
      reject.addEventListener("click", function () {
        if (window.confirm("Reject this request?")) decide("reject");
      });

      requestsBox.appendChild(card);
    });
  }

  function loadRequests(doneText) {
    AuthAPI.authedGet(
      "/institution/access-requests",
      session.access_token
    ).then(function (res) {
      if (!res.ok) {
        if (handleUnauthorized(res)) return;
        message(requestsStatus, res.data.error || "Could not load requests.", "err");
        return;
      }
      renderRequests(res.data.requests || []);
      if (doneText) message(requestsStatus, doneText, "good");
    }).catch(function () {
      message(requestsStatus, "Could not reach the API.", "err");
    });
  }

  // ---------------------------------------------------------------------
  // Student and faculty limits
  // ---------------------------------------------------------------------
  var limitsForm = document.getElementById("limits-form");
  var limitsGrid = document.getElementById("limits-grid");
  var limitsSave = document.getElementById("limits-save");
  var limitsStatus = document.getElementById("limits-status");
  var limitInputs = {};
  var limitLevels = ["easy", "medium", "hard"];
  var limitGroups = ["student", "faculty"];

  function renderLimits(data) {
    var ceiling = data.ceiling || {};
    var current = data.limits || {};
    limitsGrid.textContent = "";
    limitInputs = {};

    limitGroups.forEach(function (group) {
      var box = el("fieldset");
      box.appendChild(el("legend", "", group === "student" ? "Students" : "Faculty"));
      limitInputs[group] = {};

      limitLevels.forEach(function (level) {
        var row = el("div", "row");
        var label = el("label", "", level + " / month");
        var input = document.createElement("input");
        input.type = "number";
        input.min = "0";
        input.step = "1";
        if (ceiling[level] != null) input.max = String(ceiling[level]);
        input.id = "lim-" + group + "-" + level;
        input.value = String(
          current[group] && current[group][level] != null
            ? current[group][level] : (ceiling[level] || 0)
        );
        label.htmlFor = input.id;
        row.appendChild(label);
        row.appendChild(input);
        if (ceiling[level] != null) {
          row.appendChild(el("small", "", "max " + ceiling[level]));
        }
        box.appendChild(row);
        limitInputs[group][level] = input;
      });

      limitsGrid.appendChild(box);
    });

    limitsSave.disabled = false;
    message(limitsStatus, "");
  }

  function loadLimits() {
    AuthAPI.authedGet(
      "/institution/limits",
      session.access_token
    ).then(function (res) {
      if (!res.ok) {
        if (handleUnauthorized(res)) return;
        message(limitsStatus, res.data.error || "Could not load limits.", "err");
        return;
      }
      renderLimits(res.data);
    }).catch(function () {
      message(limitsStatus, "Could not reach the API.", "err");
    });
  }

  limitsForm.addEventListener("submit", function (event) {
    event.preventDefault();

    var body = { student: {}, faculty: {} };

    for (var g = 0; g < limitGroups.length; g++) {
      for (var l = 0; l < limitLevels.length; l++) {
        var group = limitGroups[g];
        var level = limitLevels[l];
        var raw = limitInputs[group][level].value.trim();
        var n = Number(raw);

        if (raw === "" || !Number.isInteger(n) || n < 0) {
          return message(
            limitsStatus,
            "Enter a whole number, 0 or more, for every " + group + " limit.",
            "err"
          );
        }
        body[group][level] = n;
      }
    }

    limitsSave.disabled = true;
    message(limitsStatus, "Saving…");

    AuthAPI.authedPost(
      "/institution/limits",
      body,
      session.access_token
    ).then(function (res) {
      if (res.ok) {
        message(limitsStatus, res.data.message || "Limits saved.", "good");
        return;
      }
      if (handleUnauthorized(res)) return;
      message(limitsStatus, res.data.error || "Could not save limits.", "err");
    }).catch(function () {
      message(limitsStatus, "Could not reach the API.", "err");
    }).then(function () {
      limitsSave.disabled = false;
    });
  });

  loadRequests();
  loadLimits();
  loadStatus();
})();