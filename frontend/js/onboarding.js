(function () {
  "use strict";

  var session = window.AuthAPI && AuthAPI.getSession && AuthAPI.getSession();
  if (!session) return;
  var user = session.user || {};
  var storageKey = "chaoslab_onboarding_v1:" + (user.id || user.email || "guest");
  var steps = [
    { kicker: "WELCOME TO CHAOSLAB", title: "Turn suspicious logs into clear answers.", text: "This short tour shows you how to start an incident, investigate the evidence and track your progress. You can skip it now and replay it anytime.", target: null },
    { kicker: "01 / START HERE", title: "Run your first investigation.", text: "Singleplayer gives you a seeded server incident. Choose Easy, Medium or Hard, then open the mission to inspect the virtual filesystem and logs.", target: function () { return document.querySelector('a.row[href="singleplayer.html"]'); } },
    { kicker: "02 / FOLLOW THE EVIDENCE", title: "Look for patterns, not one-off events.", text: "Read the mission brief first. Then compare source IPs, request paths and status codes. Repeated authentication failures and unusual sequences are useful clues.", target: function () { return document.querySelector('a.row[href="singleplayer.html"]'); } },
    { kicker: "03 / PLAY TOGETHER", title: "Race or learn as a group.", text: "Use Multiplayer to create or join a room. Institution users also get Classroom mode for faculty-led exercises and shared results.", target: function () { return document.querySelector('a.row[href="multiplayer.html"]'); } },
    { kicker: "04 / KNOW YOUR LIMITS", title: "Your monthly usage is visible here.", text: "These meters show how many games remain for each difficulty. Limits reset monthly, and your plan badge shows whether you are on Free, Pro or Institution.", target: function () { return document.getElementById("limits"); } },
    { kicker: "05 / KEEP IMPROVING", title: "Review your history and tune your account.", text: "History keeps your previous results. Profile lets you update your details, review your plan and request Pro access when you need higher limits.", target: function () { return document.querySelector('a.row[href="history.html"]'); } },
    { kicker: "YOU ARE READY", title: "Your first incident is waiting.", text: "Start with Singleplayer, read carefully and report every objective listed in the mission. Good investigations are methodical, reproducible and evidence-led.", target: null }
  ];
  var state = { index: 0, active: false, target: null };
  var backdrop, card, kicker, title, text, counter, dots, previous, next;

  function make(tag, className, value) { var node = document.createElement(tag); if (className) node.className = className; if (value !== undefined) node.textContent = value; return node; }
  function completed() { try { return localStorage.getItem(storageKey) === "done"; } catch (e) { return false; } }
  function markCompleted() { try { localStorage.setItem(storageKey, "done"); } catch (e) {} }

  function build() {
    backdrop = make("div", "cl-tour-backdrop"); backdrop.setAttribute("aria-hidden", "true");
    card = make("section", "cl-tour-card"); card.setAttribute("role", "dialog"); card.setAttribute("aria-modal", "true"); card.setAttribute("aria-labelledby", "cl-tour-title");
    kicker = make("div", "cl-tour-kicker"); title = make("h2", "", ""); title.id = "cl-tour-title"; text = make("p", "", "");
    var progress = make("div", "cl-tour-progress"); dots = make("div", "cl-tour-dots"); counter = make("span", ""); progress.appendChild(dots); progress.appendChild(counter);
    var actions = make("div", "cl-tour-actions"); var skip = make("button", "cl-tour-skip", "Skip tour"); skip.type = "button"; previous = make("button", "", "Back"); previous.type = "button"; next = make("button", "cl-tour-next", "Next"); next.type = "button";
    var navigation = make("div", ""); navigation.appendChild(previous); navigation.appendChild(next); actions.appendChild(skip); actions.appendChild(navigation);
    card.appendChild(kicker); card.appendChild(title); card.appendChild(text); card.appendChild(progress); card.appendChild(actions); document.body.appendChild(backdrop); document.body.appendChild(card);
    steps.forEach(function () { dots.appendChild(make("span", "cl-tour-dot")); });
    skip.addEventListener("click", close); previous.addEventListener("click", function () { if (state.index > 0) { state.index--; render(); } }); next.addEventListener("click", function () { if (state.index === steps.length - 1) close(); else { state.index++; render(); } }); window.addEventListener("resize", position); document.addEventListener("keydown", keyboard);
  }

  function keyboard(event) { if (!state.active) return; if (event.key === "Escape") close(); if (event.key === "ArrowRight" || event.key === "Enter") { event.preventDefault(); next.click(); } if (event.key === "ArrowLeft" && state.index > 0) { event.preventDefault(); previous.click(); } }
  function clearFocus() { if (state.target) state.target.classList.remove("cl-tour-focus"); state.target = null; }
  function position() {
    if (!state.active) return; var target = state.target;
    if (!target) { card.style.left = "50%"; card.style.top = "50%"; card.style.transform = "translate(-50%,-50%)"; return; }
    var rect = target.getBoundingClientRect(); var width = Math.min(420, window.innerWidth - 32); var left = Math.max(16, Math.min(rect.left, window.innerWidth - width - 16)); var top = rect.bottom + 18;
    if (top + card.offsetHeight > window.innerHeight - 16) top = rect.top - card.offsetHeight - 18; if (top < 16) top = 16; card.style.transform = "none"; card.style.left = left + "px"; card.style.top = top + "px";
  }
  function render() {
    clearFocus(); var step = steps[state.index]; kicker.textContent = step.kicker; title.textContent = step.title; text.textContent = step.text; counter.textContent = (state.index + 1) + " / " + steps.length;
    Array.prototype.forEach.call(dots.children, function (dot, index) { dot.className = "cl-tour-dot" + (index === state.index ? " active" : ""); }); previous.style.visibility = state.index === 0 ? "hidden" : "visible"; next.textContent = state.index === steps.length - 1 ? "Finish" : "Next";
    state.target = step.target && step.target(); if (state.target) { state.target.classList.add("cl-tour-focus"); state.target.scrollIntoView({ block: "center", behavior: "smooth" }); }
    requestAnimationFrame(function () { position(); card.classList.add("open"); });
  }
  function open() { if (state.active) return; state.active = true; state.index = 0; build(); render(); requestAnimationFrame(function () { backdrop.classList.add("open"); }); next.focus(); }
  function close() { if (!state.active) return; state.active = false; markCompleted(); clearFocus(); card.classList.remove("open"); backdrop.classList.remove("open"); setTimeout(function () { if (card && card.parentNode) card.parentNode.removeChild(card); if (backdrop && backdrop.parentNode) backdrop.parentNode.removeChild(backdrop); }, 200); }

  var trigger = document.getElementById("restart-tour"); if (trigger) trigger.addEventListener("click", open); if (!completed()) setTimeout(open, 650);
})();
