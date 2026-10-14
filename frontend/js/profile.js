(function () {
  "use strict";

  var session = AuthAPI.requireLogin();
  if (!session) return;

  var form = document.getElementById("profile-form");
  var msg = document.getElementById("msg");
  var button = document.getElementById("save");
  var emailInput = document.getElementById("email");
  var planInput = document.getElementById("plan");
  var nameInput = document.getElementById("display_name");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  function fill(user) {
    emailInput.value = user.email || "";
    planInput.value = user.plan || "";
    nameInput.value = user.display_name || "";
  }

  function expired() {
    AuthAPI.clearSession();
    AuthAPI.requireLogin();
  }

  document.getElementById("logout").addEventListener("click", function (event) {
    event.preventDefault();

    var done = function () {
      AuthAPI.clearSession();
      window.location.href = "login.html";
    };

    AuthAPI.logout(session.access_token).then(done, done);
  });

  fill(session.user || {});
  show("");

  var planForm = document.getElementById("plan-form");
  var planStatus = document.getElementById("plan-status");
  var planMsg = document.getElementById("plan-msg");
  var planField = document.getElementById("plan-message-field");
  var planText = document.getElementById("plan_message");
  var planButton = document.getElementById("plan-save");
  var adminLink = document.getElementById("admin-link");
  var institutionAdminLink =
    document.getElementById("institution-admin-link");

  function showPlan(text, kind) {
    planMsg.textContent = text;
    planMsg.className = "msg" + (kind ? " " + kind : "");
  }

  function setRequestForm(visible) {
    planField.style.display = visible ? "" : "none";
    planButton.style.display = visible ? "" : "none";
  }

  function renderPlan(user) {
    // These links are for presentation only.
    // API authorization must always be checked on the server.
    adminLink.style.display =
      user.role === "admin" ? "" : "none";

    if (institutionAdminLink) {
      institutionAdminLink.style.display =
        user.role === "institution_admin" ? "" : "none";
    }

    setRequestForm(false);
    instBlock.style.display = "none";
    accessBlock.style.display = "none";

    if (user.plan === "pro") {
      planStatus.textContent = "You are on the Pro plan.";
      return;
    }

    if (user.plan === "institution") {
      var inst = user.institution && user.institution.name;

      planStatus.textContent =
        "You are on your institution's plan" +
        (inst ? " (" + inst + ")" : "") + ".";

      return;
    }

    planStatus.textContent = "You are on the Free plan.";
    loadInstitutionRequest();

    AuthAPI.authedGet(
      "/plan-request",
      session.access_token
    ).then(function (res) {
      if (res.status === 401) {
        expired();
        return;
      }

      if (!res.ok) {
        planStatus.textContent =
          "You are on the Free plan. Could not load your request status.";
        return;
      }

      var req = res.data.request;

      if (req && req.status === "pending") {
        planStatus.textContent =
          "You are on the Free plan. Your Pro request is waiting for review.";
        return;
      }

      if (req && req.status === "rejected") {
        planStatus.textContent =
          "You are on the Free plan. Your last Pro request was not approved." +
          (req.decision_note ? " Note: " + req.decision_note : "") +
          " You can send a new request.";
      }

      setRequestForm(true);
    }).catch(function () {
      planStatus.textContent =
        "You are on the Free plan. Could not reach the server.";
    });
  }

  AuthAPI.me(session.access_token).then(function (res) {
    if (res.ok && res.data.user) {
      AuthAPI.updateUser(res.data.user);
      fill(res.data.user);
      renderPlan(res.data.user);
    } else if (res.status === 401) {
      expired();
    } else {
      show(
        res.data.error || "Could not load your profile.",
        "err"
      );
      planStatus.textContent = "Could not load your plan.";
    }
  }).catch(function () {
    show("Could not reach the server.", "err");
    planStatus.textContent = "Could not load your plan.";
  });

  form.addEventListener("submit", function (event) {
    event.preventDefault();

    var name = nameInput.value.trim();

    if (!name) {
      return show("Username cannot be empty.", "err");
    }

    if (name.length > 100) {
      return show("Username must be at most 100 characters.", "err");
    }

    button.disabled = true;
    show("Saving…");

    AuthAPI.authedPost(
      "/profile",
      { display_name: name },
      session.access_token
    ).then(function (res) {
      if (res.ok && res.data.user) {
        AuthAPI.updateUser(res.data.user);
        fill(res.data.user);
        show(res.data.message || "Username updated.", "good");
      } else if (res.status === 401) {
        expired();
        return;
      } else {
        show(
          res.data.error || "Could not update your username.",
          "err"
        );
      }

      button.disabled = false;
    }).catch(function () {
      show("Could not reach the server. Please try again.", "err");
      button.disabled = false;
    });
  });

  // Institution plan request (free users only)
  var instBlock = document.getElementById("inst-block");
  var instStatus = document.getElementById("inst-status");
  var instForm = document.getElementById("inst-form");
  var instMsg = document.getElementById("inst-msg");
  var instButton = document.getElementById("inst-save");
  var instName = document.getElementById("inst_name");
  var instDomains = document.getElementById("inst_domains");
  var instSeats = document.getElementById("inst_seats");
  var instMessage = document.getElementById("inst_message");

  var ownEmail = (session.user && session.user.email) || "";
  if (ownEmail.indexOf("@") > 0) {
    instDomains.placeholder = ownEmail.split("@").pop().toLowerCase();
  }

  function showInst(text, kind) {
    instMsg.textContent = text;
    instMsg.className = "msg" + (kind ? " " + kind : "");
  }

  function renderInstRequest(request) {
    var domains = (request.domains || []).join(", ");
    instBlock.style.display = "";

    if (request.status === "pending") {
      instStatus.textContent = "Your institution request for " +
        request.institution_name + " (" + domains +
        ") is waiting for review.";
      instForm.style.display = "none";
      return;
    }

    if (request.status === "approved") {
      instStatus.textContent = request.institution_name +
        " was approved. Everyone with a verified email on " + domains +
        " gets the institution plan." +
        (request.decision_note ? " Note: " + request.decision_note : "");
      instForm.style.display = "none";
      return;
    }

    instStatus.textContent = "Your last institution request for " +
      request.institution_name + " was not approved." +
      (request.decision_note ? " Note: " + request.decision_note : "") +
      " You can send a new one.";
    instForm.style.display = "";
  }

  // Access request: your email domain belongs to an institution that keeps a
  // roster, but you are not on it. The institution admin decides.
  var accessBlock = document.getElementById("access-block");
  var accessStatus = document.getElementById("access-status");
  var accessForm = document.getElementById("access-form");
  var accessMsg = document.getElementById("access-msg");
  var accessButton = document.getElementById("access-save");
  var accessRole = document.getElementById("access_role");
  var accessMessage = document.getElementById("access_message");

  function showAccess(text, kind) {
    accessMsg.textContent = text;
    accessMsg.className = "msg" + (kind ? " " + kind : "");
  }

  function renderAccess(data) {
    var inst = data.institution || "your institution";
    var req = data.request;
    accessBlock.style.display = "";

    if (req && req.status === "pending") {
      accessStatus.textContent = "Your request to join " + inst +
        " as " + req.role + " is waiting for the institution admin.";
      accessForm.style.display = "none";
      return;
    }

    var prefix = "";
    if (req && req.status === "rejected") {
      prefix = "Your last request was not approved." +
        (req.decision_note ? " Note: " + req.decision_note : "") + " ";
    }

    accessStatus.textContent = prefix + "Your email belongs to " + inst +
      ", but you are not on its roster yet. Ask the institution admin to add you.";
    accessForm.style.display = "";
  }

  function loadInstitutionRequest() {
    instBlock.style.display = "none";
    accessBlock.style.display = "none";

    AuthAPI.authedGet(
      "/institution/access-request",
      session.access_token
    ).then(function (res) {
      if (res.status === 401) {
        expired();
        return;
      }

      // Your domain already belongs to an institution: ask its admin, do not
      // register a new institution.
      if (res.ok && res.data.eligible) {
        renderAccess(res.data);
        return;
      }

      return AuthAPI.authedGet(
        "/institution-request",
        session.access_token
      ).then(function (res2) {
        if (res2.status === 401) {
          expired();
          return;
        }

        if (!res2.ok) return;

        instBlock.style.display = "";

        if (res2.data.request) {
          renderInstRequest(res2.data.request);
        } else {
          instStatus.textContent =
            "Is your school or college on ChaosLab? Request the institution plan for your email domain.";
          instForm.style.display = "";
        }
      });
    }).catch(function () {});
  }

  accessForm.addEventListener("submit", function (event) {
    event.preventDefault();

    var message = accessMessage.value.trim();

    if (message.length > 500) {
      return showAccess("Message must be at most 500 characters.", "err");
    }

    accessButton.disabled = true;
    showAccess("Sending…");

    AuthAPI.authedPost("/institution/access-request", {
      role: accessRole.value,
      message: message
    }, session.access_token).then(function (res) {
      if (res.ok) {
        showAccess("");
        accessMessage.value = "";
        renderAccess({
          institution: res.data.institution,
          request: res.data.request
        });
        accessButton.disabled = false;
        return;
      }

      if (res.status === 401) {
        expired();
        return;
      }

      showAccess(res.data.error || "Could not send the request.", "err");
      accessButton.disabled = false;
    }).catch(function () {
      showAccess("Could not reach the server. Please try again.", "err");
      accessButton.disabled = false;
    });
  });

  instForm.addEventListener("submit", function (event) {
    event.preventDefault();

    var name = instName.value.trim();
    var domains = instDomains.value.split(/[\s,;]+/).filter(Boolean);
    var seatsRaw = instSeats.value.trim();
    var seats = seatsRaw === "" ? 0 : Number(seatsRaw);

    if (name.length < 2) return showInst("Enter the institution name.", "err");
    if (!domains.length) return showInst("Enter at least one email domain.", "err");
    if (domains.length > 5) return showInst("Give at most 5 domains.", "err");
    if (!Number.isInteger(seats) || seats < 0 || seats > 1000000) {
      return showInst("Seats must be a whole number, or empty.", "err");
    }

    instButton.disabled = true;
    showInst("Sending…");

    AuthAPI.authedPost("/institution-request", {
      institution_name: name,
      domains: domains,
      seats_wanted: seats,
      message: instMessage.value.trim()
    }, session.access_token).then(function (res) {
      if (res.ok) {
        showInst("");
        renderInstRequest(res.data.request);
        instButton.disabled = false;
        return;
      }

      if (res.status === 401) {
        expired();
        return;
      }

      showInst(res.data.error || "Could not send the request.", "err");
      instButton.disabled = false;
    }).catch(function () {
      showInst("Could not reach the server. Please try again.", "err");
      instButton.disabled = false;
    });
  });

  // Pro plan request
  planForm.addEventListener("submit", function (event) {
    event.preventDefault();

    var message = planText.value.trim();

    if (message.length > 500) {
      return showPlan("Message must be at most 500 characters.", "err");
    }

    planButton.disabled = true;
    showPlan("Sending…");

    AuthAPI.authedPost(
      "/plan-request",
      { message: message },
      session.access_token
    ).then(function (res) {
      if (res.ok) {
        setRequestForm(false);
        planText.value = "";

        planStatus.textContent =
          "You are on the Free plan. Your Pro request is waiting for review.";

        showPlan(res.data.message || "Request sent.", "good");
        return;
      }

      if (res.status === 401) {
        expired();
        return;
      }

      showPlan(
        res.data.error || "Could not send your request.",
        "err"
      );

      planButton.disabled = false;
    }).catch(function () {
      showPlan("Could not reach the server. Please try again.", "err");
      planButton.disabled = false;
    });
  });

  // Change password
  var pwForm = document.getElementById("password-form");
  var pwMsg = document.getElementById("pw-msg");
  var pwButton = document.getElementById("pw-save");

  function showPw(text, kind) {
    pwMsg.textContent = text;
    pwMsg.className = "msg" + (kind ? " " + kind : "");
  }

  pwForm.addEventListener("submit", function (event) {
    event.preventDefault();

    var current = document.getElementById("current_password").value;
    var next = document.getElementById("new_password").value;
    var confirm = document.getElementById("confirm_password").value;

    if (!current) {
      return showPw("Enter your current password.", "err");
    }

    if (next.length < 12 || next.length > 72) {
      return showPw(
        "New password must be 12 to 72 characters.",
        "err"
      );
    }

    if (next !== confirm) {
      return showPw("New passwords do not match.", "err");
    }

    if (next === current) {
      return showPw(
        "New password must be different from the current one.",
        "err"
      );
    }

    pwButton.disabled = true;
    showPw("Changing password…");

    AuthAPI.authedPost(
      "/change-password",
      {
        current_password: current,
        new_password: next
      },
      session.access_token
    ).then(function (res) {
      if (res.ok) {
        AuthAPI.clearSession();

        showPw(
          res.data.message || "Password updated. Please log in again.",
          "good"
        );

        window.setTimeout(function () {
          window.location.href = "login.html";
        }, 1500);

        return;
      }

      if (res.status === 401) {
        expired();
        return;
      }

      showPw(
        res.data.error || "Could not change your password.",
        "err"
      );

      pwButton.disabled = false;
    }).catch(function () {
      showPw("Could not reach the server. Please try again.", "err");
      pwButton.disabled = false;
    });
  });

  // Delete account
  var delForm = document.getElementById("delete-form");
  var delMsg = document.getElementById("del-msg");
  var delButton = document.getElementById("del-save");

  function showDel(text, kind) {
    delMsg.textContent = text;
    delMsg.className = "msg" + (kind ? " " + kind : "");
  }

  delForm.addEventListener("submit", function (event) {
    event.preventDefault();

    var password = document.getElementById("delete_password").value;

    if (!password) {
      return showDel("Enter your password to confirm.", "err");
    }

    if (!window.confirm(
      "This permanently deletes your account and cannot be undone. Continue?"
    )) {
      return;
    }

    delButton.disabled = true;
    showDel("Deleting…");

    AuthAPI.authedPost(
      "/delete-account",
      { password: password },
      session.access_token
    ).then(function (res) {
      if (res.ok) {
        AuthAPI.clearSession();

        showDel(
          res.data.message || "Your account has been deleted.",
          "good"
        );

        window.setTimeout(function () {
          window.location.href = "index.html";
        }, 1500);

        return;
      }

      if (res.status === 401) {
        expired();
        return;
      }

      showDel(
        res.data.error || "Could not delete your account.",
        "err"
      );

      delButton.disabled = false;
    }).catch(function () {
      showDel("Could not reach the server. Please try again.", "err");
      delButton.disabled = false;
    });
  });
})();