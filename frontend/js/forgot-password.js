(function () {
  "use strict";

  var COOLDOWN_SECONDS = 60; // matches the API's minimum gap between reset emails

  var form = document.getElementById("forgot-form");
  var msg = document.getElementById("msg");
  var button = document.getElementById("send");
  var emailInput = document.getElementById("email");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  var timer = null;
  function startCooldown() {
    var left = COOLDOWN_SECONDS;
    button.disabled = true;
    button.textContent = "Send again in " + left + "s";
    clearInterval(timer);
    timer = setInterval(function () {
      left -= 1;
      if (left <= 0) {
        clearInterval(timer);
        button.disabled = false;
        button.textContent = "Send reset link";
      } else {
        button.textContent = "Send again in " + left + "s";
      }
    }, 1000);
  }

  form.addEventListener("submit", function (event) {
    event.preventDefault();
    var email = emailInput.value.trim().toLowerCase();
    if (email.indexOf("@") < 1) {
      return show("Enter a valid email address.", "err");
    }

    button.disabled = true;
    show("Sending…");

    AuthAPI.post("/forgot-password", { email: email }).then(function (res) {
      if (res.ok) {
        show(res.data.message || "If an account exists for that email, a reset link is on its way.", "good");
        startCooldown();
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
