// Navigation for the mission page. two.js is not touched.
//  1) End simulation asks "Are you sure?" first and, on Yes, leaves the page.
//     In a multiplayer race it also tells the server, so the player shows as
//     "Ended" on the scoreboard instead of "Working on it" forever. Closing the
//     browser or losing the connection does NOT do this: that player can still
//     come back to the room and reopen the mission.
//  2) After any graded submission (not only a fully correct one), exit buttons
//     appear in the submit window.
(function () {
  function $(id) { return document.getElementById(id); }

  // Multiplayer games go back to their room, classroom games to the classroom
  // page, everything else to the dashboard.
  var classroomGame = false, roomCode = "";
  try {
    var sim = JSON.parse(sessionStorage.getItem("simulationResponse") || "{}");
    classroomGame = !!sim.classroom_id;
    if (/^[A-Z0-9]{6}$/.test(sim.room_code || "")) roomCode = sim.room_code;
  } catch (e) {}
  var dest = roomCode
    ? { href: "room.html?code=" + roomCode, label: "Back to room", history: false }
    : classroomGame
      ? { href: "classroom.html", label: "Back to classroom", history: false }
      : { href: "dashboard.html", label: "Back to dashboard", history: true };

  // Multiplayer: tell the server this player is out, then go. Never gets stuck:
  // if the call is slow or fails we still leave after 4 seconds.
  function leave() {
    var go = function () { window.location.href = dest.href; };
    if (!roomCode) { go(); return; }

    var s = window.AuthAPI && AuthAPI.getSession && AuthAPI.getSession();
    if (!s) { go(); return; }

    var gone = false;
    function once() { if (!gone) { gone = true; go(); } }
    setTimeout(once, 4000);

    try { sessionStorage.removeItem("simulationResponse"); } catch (e) {}
    fetch(API.rooms + "/" + encodeURIComponent(roomCode) + "/forfeit", {
      method: "POST",
      headers: { "Authorization": "Bearer " + s.access_token },
      keepalive: true
    }).then(once, once);
  }

  // ---- 1) End simulation ----
  var abort = $("abort"), dlg = $("confirm-end"), no = $("end-no"), yes = $("end-yes");
  if (abort && dlg && no && yes) {
    var confirmed = false;

    if (roomCode) {
      var msg = dlg.querySelector(".cmsg");
      if (msg) msg.textContent = "Are you sure you want to end this simulation? You will be out of this race and cannot reopen it. Everyone else keeps playing.";
    }

    // Catch the click before two.js sees it; let it through only after Yes.
    document.addEventListener("click", function (event) {
      if (!event.target.closest("#abort")) return;
      if (confirmed) { confirmed = false; return; }
      event.preventDefault();
      event.stopPropagation();
      dlg.showModal();
    }, true);

    no.addEventListener("click", function () { dlg.close(); });

    yes.addEventListener("click", function () {
      dlg.close();
      confirmed = true;
      abort.click();          // runs the original end behaviour (stops the clock)
      abort.disabled = true;
      leave();
    });
  }

  // ---- 2) After a graded submission ----
  var result = $("dossier-result"), after = $("d-after"), back = $("d-back"), hist = $("d-history");
  if (result && after && back && hist) {
    back.href = dest.href;
    back.textContent = dest.label;
    hist.hidden = !dest.history;

    // two.js writes "SCORE n/100 - ..." here for every graded answer, right or wrong.
    new MutationObserver(function () {
      if (/^\s*SCORE\b/i.test(result.textContent)) after.hidden = false;
    }).observe(result, { childList: true, characterData: true, subtree: true });
  }
})();