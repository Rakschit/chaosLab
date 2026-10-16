// Show / hide password buttons (login and register).
// Each button points at its field with aria-controls.
(function () {
  "use strict";

  Array.prototype.forEach.call(document.querySelectorAll(".pw-toggle"), function (button) {
    var input = document.getElementById(button.getAttribute("aria-controls"));
    if (!input) return;

    button.addEventListener("click", function () {
      var reveal = input.type === "password";
      input.type = reveal ? "text" : "password";
      button.textContent = reveal ? "Hide" : "Show";
      button.setAttribute("aria-pressed", String(reveal));
      button.setAttribute("aria-label", (reveal ? "Hide" : "Show") + " password");
      input.focus();
    });
  });
})();