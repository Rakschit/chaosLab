(function () {
  "use strict";

  var nav = document.getElementById("auth-nav");
  var foot = document.getElementById("footer-auth");
  var cta = document.getElementById("cta");

  var session = AuthAPI.getSession();

  // Logged out: keep the Log in / Register links and buttons as they are.
  if (!session) return;

  function logout(event) {
    event.preventDefault();
    var done = function () {
      AuthAPI.clearSession();
      window.location.reload();
    };
    // Revoke the session on the server first, then clear it locally.
    AuthAPI.logout(session.access_token).then(done, done);
  }

  function logoutLink() {
    var a = document.createElement("a");
    a.href = "#";
    a.textContent = "Log out";
    a.addEventListener("click", logout);
    return a;
  }

  function separator() {
    var dot = document.createElement("span");
    dot.textContent = "·";
    return dot;
  }

  function render(user) {
    var name = (user && user.display_name) || "account";

    if (nav) {
      var dash = document.createElement("a");
      dash.href = "dashboard.html";
      dash.textContent = "Dashboard";
      var who = document.createElement("a");
      who.href = "profile.html";
      who.textContent = name; // textContent, never innerHTML: names are user input
      nav.textContent = "";
      nav.appendChild(dash);
      nav.appendChild(separator());
      nav.appendChild(who);
      nav.appendChild(separator());
      nav.appendChild(logoutLink());
    }
    if (cta) {
      var open = document.createElement("a");
      open.className = "btn sub";
      open.href = "dashboard.html";
      open.textContent = "Open dashboard";
      cta.textContent = "";
      cta.appendChild(open);
    }
    if (foot) {
      foot.textContent = "";
      foot.appendChild(logoutLink());
    }
  }

  // Show the saved user immediately, then confirm with the server.
  render(session.user);

  AuthAPI.me(session.access_token).then(function (res) {
    if (res.ok && res.data.user) {
      AuthAPI.updateUser(res.data.user);
      render(res.data.user);
    } else if (res.status === 401) {
      // Session expired, was revoked, or the account is no longer active.
      AuthAPI.clearSession();
      window.location.reload();
    }
  }).catch(function () { /* offline: keep the cached view */ });
})();