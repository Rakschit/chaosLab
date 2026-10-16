(function () {
  "use strict";

  var msg = document.getElementById("msg");
  var back = document.getElementById("back");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  function fail(text) {
    show(text, "err");
    back.style.display = "";
  }

  // The institution sends the token in the URL fragment, which browsers never
  // send to a server. Read it, then remove it from the address bar and history.
  var params = new URLSearchParams(window.location.hash.replace(/^#/, ""));
  var token = params.get("token") || "";
  var state = params.get("state") || "";
  try { history.replaceState(null, "", window.location.pathname); } catch (e) { /* ignore */ }

  if (!token || !state) {
    fail("This page is opened by your institution after you log in there. Start from the log in page.");
    return;
  }

  AuthAPI.post("/institution/callback", { token: token, state: state }).then(function (res) {
    if (res.ok && res.data.access_token) {
      // Institution sessions last for this browser tab session only.
      AuthAPI.saveSession(res.data, false);
      show("Logged in. Redirecting…", "good");
      var next = AuthAPI.takeNext() || "dashboard.html";
      setTimeout(function () { window.location.replace(next); }, 400);
      return;
    }
    fail(res.data.error || "Could not complete the institution login. Please start again.");
  }).catch(function () {
    fail("Could not reach the server. Please try again.");
  });
})();