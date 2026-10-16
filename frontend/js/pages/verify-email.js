(function () {
  "use strict";

  var COOLDOWN_SECONDS = 60; // matches the API's minimum gap between emails

  var dot = document.getElementById("status-dot");
  var status = document.getElementById("status");
  var statusMsg = document.getElementById("msg");
  var resendForm = document.getElementById("resend-form");
  var resendMsg = document.getElementById("resend-msg");
  var resendButton = document.getElementById("resend");
  var emailInput = document.getElementById("email");
  var loginLink = document.getElementById("login-link");

  function setText(el, text, kind) {
    el.textContent = text;
    el.className = el.className.replace(/\b(err|good)\b/g, "").trim();
    if (kind) el.className += " " + kind;
  }

  // ---- Resend ----
  var rememberedEmail = AuthAPI.recallEmail();
  if (rememberedEmail) emailInput.value = rememberedEmail;

  var timer = null;
  function startCooldown() {
    var left = COOLDOWN_SECONDS;
    resendButton.disabled = true;
    resendButton.textContent = "Resend in " + left + "s";
    clearInterval(timer);
    timer = setInterval(function () {
      left -= 1;
      if (left <= 0) {
        clearInterval(timer);
        resendButton.disabled = false;
        resendButton.textContent = "Resend verification link";
      } else {
        resendButton.textContent = "Resend in " + left + "s";
      }
    }, 1000);
  }

  resendForm.addEventListener("submit", function (event) {
    event.preventDefault();
    var email = emailInput.value.trim().toLowerCase();
    if (email.indexOf("@") < 1) {
      return setText(resendMsg, "Enter the email you registered with.", "err");
    }
    resendButton.disabled = true;
    setText(resendMsg, "Sending…");

    AuthAPI.post("/resend-verification", { email: email }).then(function (res) {
      if (res.ok) {
        AuthAPI.rememberEmail(email);
        setText(resendMsg, res.data.message || "If an unverified account exists, a new link is on its way.", "good");
        startCooldown();
        return;
      }
      setText(resendMsg, res.data.error || "Something went wrong. Please try again.", "err");
      resendButton.disabled = false;
    }).catch(function () {
      setText(resendMsg, "Could not reach the server. Please try again.", "err");
      resendButton.disabled = false;
    });
  });

  // ---- Verify, when opened from the email link (?token=...) ----
  var token = new URLSearchParams(window.location.search).get("token");

  if (!token) {
    // Arrived from the register page: tell the user to check their inbox.
    var shown = document.getElementById("email-shown");
    if (rememberedEmail) shown.textContent = rememberedEmail;
    return;
  }

  // Keep the one-time token out of the address bar, history and Referer headers.
  try { window.history.replaceState(null, "", window.location.pathname); } catch (e) { /* ignore */ }

  document.getElementById("pending").style.display = "none";
  setText(statusMsg, "Verifying your email…");

  AuthAPI.post("/verify-email", { token: token }).then(function (res) {
    if (res.ok) {
      setText(status, "Email verified.", "ok");
      setText(statusMsg, res.data.message || "You can now log in.", "good");
      resendForm.style.display = "none";
      loginLink.style.display = "";
      return;
    }
    dot.className = "dot bad";
    setText(status, "This link did not work.", "bad");
    setText(statusMsg, res.data.error || "The verification link is invalid or has expired.", "err");
    loginLink.style.display = "";
  }).catch(function () {
    dot.className = "dot bad";
    setText(status, "Could not verify.", "bad");
    setText(statusMsg, "Could not reach the server. Reload this page to try again.", "err");
  });
})();
