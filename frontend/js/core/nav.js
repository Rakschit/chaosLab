// ChaosLab shared navbar.
//
// Put <div id="site-nav"></div> where the old header was, load js/auth-api.js
// first, then this file. The navbar is built from the saved login session, so
// it shows the right links for logged-out visitors, players, institution
// players, institution admins and admins. The server still checks every role;
// this only decides which links to show.
(function () {
  "use strict";

  var mount = document.getElementById("site-nav");
  if (!mount || !window.AuthAPI) return;

  var session = AuthAPI.getSession();
  var page = (window.location.pathname.split("/").pop() || "index.html").toLowerCase();
  // Pages that belong to another page's section highlight that section.
  var active = { "classroom-results.html": "classroom.html", "room.html": "multiplayer.html" }[page] || page;
  var state = { open: false, menu: false };

  function el(tag, className, text, attrs) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined && text !== null) node.textContent = text; // textContent only: names are user input
    if (attrs) for (var k in attrs) node.setAttribute(k, attrs[k]);
    return node;
  }

  function link(className, href, text) {
    var a = el("a", className, text, { href: href });
    if (active === href) a.setAttribute("aria-current", "page");
    return a;
  }

  function planTag(plan) {
    if (plan === "institution") return el("span", "cl-tag blue", "Institution");
    if (plan === "pro") return el("span", "cl-tag ok", "Pro");
    return el("span", "cl-tag", "Free");
  }

  function build() {
    var user = session && session.user ? session.user : null;
    var loggedIn = !!session;

    var nav = el("div", "cl-nav" + (state.open ? " open" : ""));
    var inner = el("div", "cl-in");

    // brand (the page scripts that need the status dot ask for it with data-status-dot)
    var brand = el("a", "cl-brand", null, { href: loggedIn ? "dashboard.html" : "index.html" });
    var dot = el("span", "dot");
    if (mount.hasAttribute("data-status-dot")) dot.id = "status-dot";
    brand.appendChild(dot);
    brand.appendChild(el("span", "", "ChaosLab"));
    inner.appendChild(brand);

    inner.appendChild(el("button", "cl-burger", null, {
      type: "button", "aria-label": "Menu", "aria-expanded": String(state.open), "data-cl-burger": ""
    })).appendChild(el("i"));

    var collapse = el("div", "cl-collapse");
    var links = el("div", "cl-links", null, { role: "navigation", "aria-label": "Main" });
    var right = el("div", "cl-right");

    if (!loggedIn) {
      links.appendChild(link("", "index.html", "Home"));
      right.appendChild(link("cl-btn", "login.html", "Log in"));
      right.appendChild(link("cl-btn ok", "register.html", "Register"));
    } else {
      links.appendChild(link("", "dashboard.html", "Dashboard"));
      links.appendChild(link("", "singleplayer.html", "Singleplayer"));
      links.appendChild(link("", "multiplayer.html", "Multiplayer"));
      if (user && user.plan === "institution") links.appendChild(link("", "classroom.html", "Classroom"));
      links.appendChild(link("", "history.html", "History"));

      var name = (user && user.display_name) || "account";
      var menu = el("div", "cl-menu" + (state.menu ? " open" : ""));
      var button = el("button", "cl-menu-btn", null, {
        type: "button", "aria-haspopup": "true", "aria-expanded": String(state.menu), "data-cl-menu": ""
      });
      button.appendChild(el("span", "", name, { id: "who" }));
      menu.appendChild(button);

      var panel = el("div", "cl-panel");
      var who = el("div", "cl-who");
      who.appendChild(el("b", "", name));
      who.appendChild(planTag(user && user.plan));
      panel.appendChild(who);
      panel.appendChild(link("cl-item", "profile.html", "Profile"));

      var role = user && user.role;
      if (role === "institution_admin" || role === "admin") {
        panel.appendChild(el("div", "cl-grp", "Manage"));
        if (role === "institution_admin") panel.appendChild(link("cl-item", "institution-admin.html", "Institution admin"));
        if (role === "admin") {
          panel.appendChild(link("cl-item", "admin.html", "Pro requests"));
          panel.appendChild(link("cl-item", "admin-institutions.html", "Institutions"));
        }
      }
      panel.appendChild(el("div", "cl-sep"));
      panel.appendChild(el("a", "cl-item out", "Log out", { href: "#", id: "logout" }));
      menu.appendChild(panel);
      right.appendChild(menu);
    }

    collapse.appendChild(links);
    collapse.appendChild(right);
    inner.appendChild(collapse);
    nav.appendChild(inner);
    return nav;
  }

  function render() {
    mount.textContent = "";
    mount.appendChild(build());
  }

  // Open / close the burger and the account menu.
  document.addEventListener("click", function (event) {
    if (event.target.closest("[data-cl-burger]")) {
      state.open = !state.open;
      return render();
    }
    if (event.target.closest("[data-cl-menu]")) {
      state.menu = !state.menu;
      return render();
    }
    if (state.menu && !event.target.closest(".cl-menu")) {
      state.menu = false;
      render();
    }
  });

  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape" && (state.menu || state.open)) {
      state.menu = false;
      state.open = false;
      render();
    }
  });

  // One logout for every page. It runs before any page-level handler (which then
  // never fires), so logging out behaves the same everywhere and only happens once.
  document.addEventListener("click", function (event) {
    if (!event.target.closest("#logout")) return;
    event.preventDefault();
    event.stopPropagation();
    var current = AuthAPI.getSession();
    var done = function () {
      AuthAPI.clearSession();
      window.location.href = "login.html";
    };
    if (!current) return done();
    AuthAPI.logout(current.access_token).then(done, done);
  }, true);

  // Pages refresh the saved user (plan, role, name) after asking the server.
  // Re-draw the navbar when that happens so it never shows stale links.
  if (typeof AuthAPI.updateUser === "function") {
    var original = AuthAPI.updateUser;
    AuthAPI.updateUser = function () {
      var result = original.apply(this, arguments);
      session = AuthAPI.getSession();
      render();
      return result;
    };
  }

  render();
})();
