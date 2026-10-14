// Only logged-in users may play. requireLogin() redirects to login.html and
// remembers this page, so the player comes back here afterwards.
const session = AuthAPI.requireLogin();
if (!session) throw new Error("Login required.");

const environments = ["nginx"];
const difficulties = ["easy", "medium", "hard"];
let difficultyIndex = 0;

const seed = document.getElementById("seed");
const difficulty = document.getElementById("difficulty");
const log = document.getElementById("log");

function randomSeed() {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return [...bytes].map(b => b.toString(16).padStart(2, "0")).join("");
}

function render() {
  document.getElementById("envs").innerHTML =
    environments.map(e => `<div>› ${e}</div>`).join("");

  const level = difficulties[difficultyIndex];
  difficulty.textContent = level;
  document.getElementById("go").disabled = false;
}

function addLog(text) {
  const line = document.createElement("div");
  line.textContent = text;
  log.appendChild(line);
}

let starting = false;

async function startSimulation() {
  if (starting) return;

  if (seed.value.trim() && !/^[0-9a-fA-F]{16}$/.test(seed.value.trim())) {
    addLog("warning: seed must be 16 hex characters - the server will generate a new one");
  }

  starting = true;
  const body = {
    environment: "nginx",
    difficulty: difficulties[difficultyIndex],
    seed: seed.value.trim()
  };

  addLog("system: sending simulation to API...");

  try {
    const response = await fetch(API.singleplayer, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": "Bearer " + session.access_token
      },
      body: JSON.stringify(body)
    });

    // Session expired or revoked: log in again, then come back here.
    if (response.status === 401) {
      AuthAPI.clearSession();
      AuthAPI.requireLogin();
      return;
    }

    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "API request failed");
    }

    // Pass the complete API response to the next page.
    sessionStorage.setItem("simulationResponse", JSON.stringify(data));

    // Open the separate response page.
    window.location.href = "mission.html";

  } catch (error) {
    addLog("error: " + error.message);
  } finally {
    starting = false;
  }
}

document.getElementById("reseed").onclick = () => {
  seed.value = randomSeed();
};

document.getElementById("prev").onclick = () => {
  difficultyIndex =
    (difficultyIndex - 1 + difficulties.length) % difficulties.length;
  render();
};

document.getElementById("next").onclick = () => {
  difficultyIndex =
    (difficultyIndex + 1) % difficulties.length;
  render();
};

document.getElementById("go").onclick = startSimulation;

document.addEventListener("keydown", e => {
  if (e.key === "Enter" && e.target.tagName !== "BUTTON") startSimulation();
});

seed.value = randomSeed();
render();