(function () {
  "use strict";

  // Only logged-in users may see this page.
  var session = AuthAPI.requireLogin();
  if (!session) return;

  var form = document.getElementById("request-form");
  var statusBox = document.getElementById("plan-status");
  var msg = document.getElementById("msg");
  var button = document.getElementById("send");
  var nameInput = document.getElementById("institution_name");
  var domainsInput = document.getElementById("domains");
  var seatsInput = document.getElementById("seats");
  var messageInput = document.getElementById("message");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  function expired() {
    AuthAPI.clearSession();
    AuthAPI.requireLogin(); // remembers this page, then goes to login
  }

  document.getElementById("logout").addEventListener("click", function (event) {
    event.preventDefault();
    var done = function () {
      AuthAPI.clearSession();
      window.location.href = "login.html";
    };
    AuthAPI.logout(session.access_token).then(done, done);
  });

  // Hint: one of the domains must match the account email.
  var email = (session.user && session.user.email) || "";
  if (email.indexOf("@") > 0) {
    domainsInput.placeholder = email.split("@").pop().toLowerCase();
  }

  function renderRequest(request) {
    var domains = (request.domains || []).join(", ");

    if (request.status === "pending") {
      statusBox.textContent = "Your request for " + request.institution_name + " (" + domains +
        ") is waiting for review. You will get an email when it is decided.";
      form.style.display = "none";
      return;
    }
    if (request.status === "approved") {
      statusBox.textContent = request.institution_name + " was approved. Everyone with a verified email on " +
        domains + " is on the institution plan." +
        (request.decision_note ? " Note: " + request.decision_note : "");
      form.style.display = "none";
      return;
    }
    // rejected: the user may try again
    statusBox.textContent = "Your last request for " + request.institution_name + " was not approved." +
      (request.decision_note ? " Note: " + request.decision_note : "") + " You can send a new one.";
    form.style.display = "";
  }

  AuthAPI.authedGet("/institution-request", session.access_token).then(function (res) {
    if (res.ok) {
      if (res.data.request) {
        renderRequest(res.data.request);
      } else {
        statusBox.textContent = "You have not sent an institution request yet.";
        form.style.display = "";
      }
    } else if (res.status === 401) {
      expired();
    } else {
      statusBox.textContent = res.data.error || "Could not load your request.";
    }
  }).catch(function () {
    statusBox.textContent = "Could not reach the server.";
  });

  form.addEventListener("submit", function (event) {
    event.preventDefault();

    var name = nameInput.value.trim();
    var domains = domainsInput.value.split(/[\s,;]+/).filter(Boolean);
    var seatsRaw = seatsInput.value.trim();
    var seats = seatsRaw === "" ? 0 : Number(seatsRaw);

    if (name.length < 2) return show("Enter the institution name.", "err");
    if (!domains.length) return show("Enter at least one email domain.", "err");
    if (domains.length > 5) return show("Give at most 5 domains.", "err");
    if (!Number.isInteger(seats) || seats < 0 || seats > 1000000) {
      return show("Seats must be a whole number, or empty.", "err");
    }

    button.disabled = true;
    show("Sending…");

    AuthAPI.authedPost("/institution-request", {
      institution_name: name,
      domains: domains,
      seats_wanted: seats,
      message: messageInput.value.trim()
    }, session.access_token).then(function (res) {
      if (res.ok) {
        show("");
        renderRequest(res.data.request);
        return;
      }
      if (res.status === 401) { expired(); return; }
      show(res.data.error || "Could not send the request.", "err");
      button.disabled = false;
    }).catch(function () {
      show("Could not reach the server. Please try again.", "err");
      button.disabled = false;
    });
  });
})();
