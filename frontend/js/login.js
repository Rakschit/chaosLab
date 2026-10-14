(function () {
  "use strict";

  var form = document.getElementById("login-form");
  var msg = document.getElementById("msg");
  var button = form.querySelector("button[type=submit]");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  // Already signed in: skip the form.
  if (AuthAPI.getSession()) {
    window.location.replace(AuthAPI.takeNext() || "dashboard.html");
    return;
  }

  form.addEventListener("submit", function (event) {
    event.preventDefault();

    var email = document.getElementById("email").value.trim().toLowerCase();
    var password = document.getElementById("password").value;
    var remember = form.elements["remember"].checked;

    if (email.indexOf("@") < 1) {
      return show("Enter a valid email address.", "err");
    }
    if (!password) {
      return show("Enter your password.", "err");
    }

    button.disabled = true;
    show("Logging in…");

    AuthAPI.post("/login", { email: email, password: password }).then(function (res) {
      if (res.ok && res.data.access_token) {
        AuthAPI.saveSession(res.data, remember);
        show("Logged in. Redirecting…", "good");
        var next = AuthAPI.takeNext() || "dashboard.html";
        setTimeout(function () { window.location.href = next; }, 600);
        return;
      }

      if (res.status === 403 && res.data.error === "email not verified") {
        AuthAPI.rememberEmail(email);
        show("Your email is not verified yet. Redirecting to verification…", "err");
        setTimeout(function () { window.location.href = "verify-email.html"; }, 1500);
        return;
      }

      show(res.data.error || "Something went wrong. Please try again.", "err");
      button.disabled = false;
    }).catch(function () {
      show("Could not reach the server. Please try again.", "err");
      button.disabled = false;
    });
  });

  // Login through an institution: the institution's own login page
  // authenticates the person and sends them back to institution-callback.html.
  var instForm = document.getElementById("institution-form");
  var instMsg = document.getElementById("institution-msg");
  var instButton = instForm.querySelector("button[type=submit]");

  function showInst(text, kind) {
    instMsg.textContent = text;
    instMsg.className = "msg" + (kind ? " " + kind : "");
  }

  instForm.addEventListener("submit", function (event) {
    event.preventDefault();
    var value = document.getElementById("institution").value.trim().toLowerCase();
    if (!value) return showInst("Enter your institution code or email.", "err");

    instButton.disabled = true;
    showInst("Contacting your institution…");

    AuthAPI.post("/institution/start", { institution: value }).then(function (res) {
      var target = res.ok && res.data.redirect_url;
      // Only follow an https URL returned by our own API.
      if (typeof target === "string" && /^https:\/\//i.test(target)) {
        showInst("Redirecting to " + (res.data.institution || "your institution") + "…", "good");
        window.location.href = target;
        return;
      }
      showInst(res.data.error || "Institution login is not available.", "err");
      instButton.disabled = false;
    }).catch(function () {
      showInst("Could not reach the server. Please try again.", "err");
      instButton.disabled = false;
    });
  });
})();