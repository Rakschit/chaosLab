(function () {
  "use strict";

  // Only logged-in users may see this page. The server checks the admin role on
  // every call; this only keeps other users out of the UI.
  var session = AuthAPI.requireLogin();
  if (!session) return;

  var msg = document.getElementById("msg");
  var list = document.getElementById("list");
  var count = document.getElementById("count");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  function expired() {
    AuthAPI.clearSession();
    AuthAPI.requireLogin();
  }

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text; // never innerHTML
    return node;
  }

  document.getElementById("logout").addEventListener("click", function (event) {
    event.preventDefault();
    var done = function () { AuthAPI.clearSession(); window.location.href = "login.html"; };
    AuthAPI.logout(session.access_token).then(done, done);
  });

  // Run one admin call and report the result in `status`.
  function call(path, body, status, onDone) {
    status.textContent = "Saving…";
    status.className = "msg";
    AuthAPI.authedPost(path, body, session.access_token).then(function (res) {
      if (res.status === 401) { expired(); return; }
      if (res.status === 403) { window.location.replace("profile.html"); return; }
      if (res.ok) {
        status.textContent = res.data.message || "Saved.";
        status.className = "msg good";
        if (onDone) onDone(res.data);
        return;
      }
      status.textContent = res.data.error || "Could not save.";
      status.className = "msg err";
    }).catch(function () {
      status.textContent = "Could not reach the server. Please try again.";
      status.className = "msg err";
    });
  }

  function fieldRow(labelText, input) {
    var f = el("div", "field");
    f.appendChild(el("label", "", labelText));
    f.appendChild(input);
    return f;
  }

  function renderCard(it) {
    var card = el("div", "card");
    card.appendChild(el("h3", "", it.name + " · " + it.slug));

    var license = it.license_active
      ? "License active" + (it.license_expires_at ? " until " + new Date(it.license_expires_at).toLocaleDateString() : " (no expiry)")
      : "No active license";
    var lim = it.monthly_limits || {};
    card.appendChild(el("p", "dim",
      it.status + " · " + it.auth_mode + " login · " + license));
    card.appendChild(el("p", "",
      "Per month: easy " + lim.easy + ", medium " + lim.medium + ", hard " + lim.hard +
      " · seats " + (it.max_seats || "unlimited")));
    if (it.auth_mode === "token") {
      card.appendChild(el("p", "dim",
        "Login URL: " + (it.login_url || "(none)") + " · public key " + (it.has_public_key ? "set" : "MISSING")));
    }
    card.appendChild(el("p", "",
      "Domains: " + (it.domains.length ? it.domains.join(", ") : "(none)")));

    var status = el("div", "msg");

    // Add a domain.
    var domainInput = document.createElement("input");
    domainInput.type = "text";
    domainInput.placeholder = "sicsr.ac.in";
    domainInput.maxLength = 253;
    var addDomain = el("button", "btn sub", "Add domain");
    addDomain.type = "button";
    addDomain.addEventListener("click", function () {
      call("/admin/institutions/domain",
        { institution_id: it.id, domain: domainInput.value.trim().toLowerCase() }, status,
        function () { load(); });
    });

    // Grant or revoke a license.
    var emailInput = document.createElement("input");
    emailInput.type = "email";
    emailInput.placeholder = it.contact_email || "billing contact email";
    emailInput.value = it.contact_email || "";
    var dateInput = document.createElement("input");
    dateInput.type = "date";
    var grant = el("button", "btn sub", "Grant license");
    grant.type = "button";
    grant.addEventListener("click", function () {
      var body = { institution_id: it.id, action: "grant", license_email: emailInput.value.trim() };
      if (dateInput.value) body.expires_at = new Date(dateInput.value + "T23:59:59Z").toISOString();
      call("/admin/institutions/license", body, status, function () { load(); });
    });
    var revoke = el("button", "btn abort", "Revoke license");
    revoke.type = "button";
    revoke.addEventListener("click", function () {
      if (!window.confirm("Revoke the license for " + it.name + "? Its users fall back to the free plan.")) return;
      call("/admin/institutions/license", { institution_id: it.id, action: "revoke" }, status,
        function () { load(); });
    });

    card.appendChild(fieldRow("Add an email domain", domainInput));
    card.appendChild(addDomain);
    card.appendChild(fieldRow("License contact email", emailInput));
    card.appendChild(fieldRow("Expires (optional)", dateInput));
    var row = el("div", "split");
    row.appendChild(grant);
    row.appendChild(revoke);
    card.appendChild(status);
    card.appendChild(row);
    return card;
  }

  function load() {
    AuthAPI.authedGet("/admin/institutions", session.access_token).then(function (res) {
      if (res.status === 401) { expired(); return; }
      if (res.status === 403) { window.location.replace("profile.html"); return; }
      if (!res.ok) { show(res.data.error || "Could not load institutions.", "err"); return; }
      list.textContent = "";
      var items = res.data.institutions || [];
      items.forEach(function (it) { list.appendChild(renderCard(it)); });
      count.textContent = items.length ? items.length + " total" : "";
      show(items.length ? "" : "No institutions yet.");
    }).catch(function () {
      show("Could not reach the server.", "err");
    });
  }

  // Create form.
  var form = document.getElementById("create-form");
  var createMsg = document.getElementById("create-msg");
  var modeSelect = document.getElementById("c-mode");

  function syncMode() {
    var token = modeSelect.value === "token";
    document.getElementById("c-url-field").style.display = token ? "" : "none";
    document.getElementById("c-key-field").style.display = token ? "" : "none";
  }
  modeSelect.addEventListener("change", syncMode);
  syncMode();

  function num(id) { return parseInt(document.getElementById(id).value, 10); }

  form.addEventListener("submit", function (event) {
    event.preventDefault();
    var limits = { easy: num("c-easy"), medium: num("c-medium"), hard: num("c-hard") };
    if (Object.keys(limits).some(function (k) { return isNaN(limits[k]); }) || isNaN(num("c-seats"))) {
      createMsg.textContent = "Limits and seats must be numbers.";
      createMsg.className = "msg err";
      return;
    }
    var token = modeSelect.value === "token";
    var body = {
      name: document.getElementById("c-name").value.trim(),
      slug: document.getElementById("c-slug").value.trim().toLowerCase(),
      auth_mode: modeSelect.value,
      login_url: token ? document.getElementById("c-url").value.trim() : "",
      public_key: token ? document.getElementById("c-key").value.trim() : "",
      monthly_limits: limits,
      max_seats: num("c-seats"),
      contact_email: document.getElementById("c-contact").value.trim()
    };
    call("/admin/institutions", body, createMsg, function () {
      form.reset();
      syncMode();
      load();
    });
  });

  load();
})();