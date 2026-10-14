(function () {
  "use strict";

  var form = document.getElementById("register-form");
  var msg = document.getElementById("msg");
  var button = form.querySelector("button[type=submit]");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  function byteLength(s) {
    return new TextEncoder().encode(s).length;
  }

  form.addEventListener("submit", function (event) {
    event.preventDefault();

    var displayName = document.getElementById("display_name").value.trim();
    var email = document.getElementById("email").value.trim().toLowerCase();
    var password = document.getElementById("password").value;
    var confirm = document.getElementById("confirm").value;
    var terms = form.elements["terms"].checked;

    // These mirror the API's rules so people get instant feedback;
    // the server checks everything again.
    if (!displayName || Array.from(displayName).length > 100) {
      return show("Display name is required (max 100 characters).", "err");
    }
    if (email.indexOf("@") < 1) {
      return show("Enter a valid email address.", "err");
    }
    if (byteLength(password) < 12 || byteLength(password) > 72) {
      return show("Password must be between 12 and 72 characters.", "err");
    }
    if (password !== confirm) {
      return show("Passwords do not match.", "err");
    }
    if (!terms) {
      return show("Please agree to the terms of use.", "err");
    }

    button.disabled = true;
    show("Creating account…");

    AuthAPI.post("/register", {
      email: email,
      password: password,
      display_name: displayName
    }).then(function (res) {
      if (res.ok) {
        AuthAPI.rememberEmail(email);
        show(res.data.message || "Check your email for a verification link.", "good");
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
})();
