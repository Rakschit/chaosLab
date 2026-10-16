(function () {
  "use strict";

  // Where the auth API lives. Change this when you deploy:
  //   deployed on Vercel     -> "https://YOUR-PROJECT.vercel.app"  (no trailing slash)
  //   local `vercel dev`     -> "http://127.0.0.1:3000"
  var API_BASE = "https://auth-api-zeta-three.vercel.app"; // <-- change to YOUR Vercel URL
  var PREFIX = "/api/v1/auth";

  // POST a JSON body and resolve to { ok, status, data }.
  // Rejects only when the server cannot be reached at all.
  function post(path, body) {
    return fetch(API_BASE + PREFIX + path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { ok: res.ok, status: res.status, data: data };
      });
    });
  }

  // POST with the Bearer token; same result shape as post().
  function authedPost(path, body, token) {
    return fetch(API_BASE + PREFIX + path, {
      method: "POST",
      headers: { "Content-Type": "application/json", "Authorization": "Bearer " + token },
      body: JSON.stringify(body)
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { ok: res.ok, status: res.status, data: data };
      });
    });
  }

  // GET any path with the Bearer token; same result shape as post().
  function authedGet(path, token) {
    return fetch(API_BASE + PREFIX + path, {
      method: "GET",
      headers: { "Authorization": "Bearer " + token }
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { ok: res.ok, status: res.status, data: data };
      });
    });
  }

  function submitFeedback(body, token) {
    return authedPost("/feedback", body, token);
  }

  // GET /me with the Bearer token; same result shape as post().
  function me(token) {
    return fetch(API_BASE + PREFIX + "/me", {
      method: "GET",
      headers: { "Authorization": "Bearer " + token }
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { ok: res.ok, status: res.status, data: data };
      });
    });
  }

  // Revoke the session on the server (Redis). Resolves even if the call fails,
  // so the caller can always clear the local copy afterwards.
  function logout(token) {
    return fetch(API_BASE + PREFIX + "/logout", {
      method: "POST",
      headers: { "Authorization": "Bearer " + token }
    }).catch(function () { /* offline: still log out locally */ });
  }

  // Remember the address between pages (never the password).
  function rememberEmail(email) {
    try { sessionStorage.setItem("pendingEmail", email); } catch (e) { /* storage unavailable */ }
  }
  function recallEmail() {
    try { return sessionStorage.getItem("pendingEmail") || ""; } catch (e) { return ""; }
  }

  // Login session. "Keep me signed in" uses localStorage (survives closing the
  // browser); otherwise sessionStorage (cleared when the tab closes).
  var SESSION_KEY = "authSession";

  function saveSession(data, remember) {
    clearSession();
    try {
      (remember ? localStorage : sessionStorage).setItem(SESSION_KEY, JSON.stringify({
        access_token: data.access_token,
        expires_at: data.expires_at,
        user: data.user
      }));
    } catch (e) { /* storage unavailable */ }
  }

  function getSession() {
    try {
      var raw = localStorage.getItem(SESSION_KEY) || sessionStorage.getItem(SESSION_KEY);
      if (!raw) return null;
      var session = JSON.parse(raw);
      if (!session || !session.access_token) return null;
      if (session.expires_at && Date.parse(session.expires_at) <= Date.now()) {
        clearSession();
        return null;
      }
      return session;
    } catch (e) { return null; }
  }

  function clearSession() {
    try { localStorage.removeItem(SESSION_KEY); } catch (e) { /* ignore */ }
    try { sessionStorage.removeItem(SESSION_KEY); } catch (e) { /* ignore */ }
  }

  // Replace the saved user (e.g. after a username change), keeping the token
  // in whichever storage it already lives in.
  function updateUser(user) {
    [localStorage, sessionStorage].forEach(function (store) {
      try {
        var raw = store.getItem(SESSION_KEY);
        if (!raw) return;
        var session = JSON.parse(raw);
        session.user = user;
        store.setItem(SESSION_KEY, JSON.stringify(session));
      } catch (e) { /* ignore */ }
    });
  }

  // Remember which page the user wanted, so login can send them back to it.
  // Only plain "name.html" values are accepted, which blocks open redirects.
  var NEXT_KEY = "afterLogin";
  function setNext(page) {
    if (/^[a-z0-9_-]+\.html$/i.test(page)) {
      try { sessionStorage.setItem(NEXT_KEY, page); } catch (e) { /* ignore */ }
    }
  }
  function takeNext() {
    try {
      var page = sessionStorage.getItem(NEXT_KEY) || "";
      sessionStorage.removeItem(NEXT_KEY);
      return /^[a-z0-9_-]+\.html$/i.test(page) ? page : "";
    } catch (e) { return ""; }
  }

  // Call at the top of any page that needs a logged-in user. Returns the
  // session, or sends the visitor to the login page and returns null.
  function requireLogin() {
    var session = getSession();
    if (session) return session;
    var page = window.location.pathname.split("/").pop();
    setNext(page);
    window.location.replace("login.html");
    return null;
  }

  window.AuthAPI = {
    authedPost: authedPost,
    authedGet: authedGet,
    submitFeedback: submitFeedback,
    submitFeedback: submitFeedback,
    updateUser: updateUser,
    setNext: setNext,
    takeNext: takeNext,
    requireLogin: requireLogin,
    post: post,
    me: me,
    logout: logout,
    rememberEmail: rememberEmail,
    recallEmail: recallEmail,
    saveSession: saveSession,
    getSession: getSession,
    clearSession: clearSession
  };
})();
