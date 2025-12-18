
import * as THREE from "https://unpkg.com/three@0.160.0/build/three.module.js";
import { OrbitControls } from "https://unpkg.com/three@0.160.0/examples/jsm/controls/OrbitControls.js";

/**
 * -------- JSON validation (simple + strict enough) ----------
 */
function validateRepos(data) {
  const errors = [];
  if (!Array.isArray(data)) {
    errors.push("Root must be an array of repos.");
    return { ok: false, errors };
  }

  const required = ["name", "full_name", "size_kb", "size_readable", "updated_at", "html_url"];
  data.forEach((r, i) => {
    if (typeof r !== "object" || r === null) {
      errors.push(`Repo[${i}] must be an object.`);
      return;
    }
    required.forEach((k) => {
      if (!(k in r)) errors.push(`Repo[${i}] missing required field: ${k}`);
    });

    if (typeof r.name !== "string") errors.push(`Repo[${i}].name must be string`);
    if (typeof r.full_name !== "string") errors.push(`Repo[${i}].full_name must be string`);
    if (typeof r.size_kb !== "number") errors.push(`Repo[${i}].size_kb must be number`);
    if (typeof r.size_readable !== "string") errors.push(`Repo[${i}].size_readable must be string`);
    if (typeof r.updated_at !== "string") errors.push(`Repo[${i}].updated_at must be string`);
    if (typeof r.html_url !== "string") errors.push(`Repo[${i}].html_url must be string`);

    // Enrichment fields are optional, but if present check shape
    if ("weekly_commits_52w" in r && !Array.isArray(r.weekly_commits_52w)) {
      errors.push(`Repo[${i}].weekly_commits_52w must be array if present`);
    }
  });

  return { ok: errors.length === 0, errors };
}

function showError(errors, raw) {
  const el = document.getElementById("error");
  const pre = document.getElementById("errorText");
  el.style.display = "grid";
  pre.textContent =
    errors.join("\n") +
    "\n\n--- First 1 repo (debug) ---\n" +
    JSON.stringify(raw?.[0] ?? raw, null, 2);
}

/**
 * -------- Visual design helpers ----------
 */
const languagePalette = new Map([
  ["TypeScript", 0x4da3ff],
  ["JavaScript", 0xffd166],
  ["Go", 0x00d4ff],
  ["Rust", 0xff6b6b],
  ["Python", 0x9b5de5],
  ["C++", 0x5ef38c],
  ["C", 0x7bdff2],
  ["HTML", 0xff5ea8],
  ["CSS", 0x6ee7ff],
  ["Shell", 0xd0f4de],
  ["Kotlin", 0xff8fab],
  ["Swift", 0xffc6ff],
]);

function colorForLanguage(lang) {
  if (!lang) return 0x9aa4ff;
  if (languagePalette.has(lang)) return languagePalette.get(lang);
  // deterministic fallback: hash string -> hue-ish RGB
  let h = 0;
  for (let i = 0; i < lang.length; i++) h = (h * 31 + lang.charCodeAt(i)) >>> 0;
  const r = 80 + (h & 0x7f);
  const g = 80 + ((h >> 7) & 0x7f);
  const b = 120 + ((h >> 14) & 0x7f);
  return (r << 16) | (g << 8) | b;
}

function clamp01(x) {
  return Math.max(0, Math.min(1, x));
}

function safeParseDate(s) {
  const t = Date.parse(s);
  return Number.isFinite(t) ? new Date(t) : null;
}

function logScale(value, k = 1) {
  // nice compression so giant repos don't dominate
  return Math.log(1 + Math.max(0, value)) / Math.log(1 + k);
}

/**
 * -------- Load JSON ----------
 */
const jsonUrl = "./repos_index_enriched.json";
const repos = await fetch(jsonUrl).then((r) => r.json());

const v = validateRepos(repos);
if (!v.ok) {
  showError(v.errors, repos);
  throw new Error("Invalid JSON schema");
}

/**
 * -------- Scene setup ----------
 */
const renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: "high-performance" });
renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
renderer.setSize(innerWidth, innerHeight);
renderer.outputColorSpace = THREE.SRGBColorSpace;
document.body.appendChild(renderer.domElement);

const scene = new THREE.Scene();
scene.fog = new THREE.FogExp2(0x05060a, 0.025);

const camera = new THREE.PerspectiveCamera(55, innerWidth / innerHeight, 0.1, 2000);
camera.position.set(0, 65, 110);

const controls = new OrbitControls(camera, renderer.domElement);
controls.enableDamping = true;
controls.dampingFactor = 0.06;
controls.target.set(0, 10, 0);
controls.minDistance = 15;
controls.maxDistance = 220;
controls.maxPolarAngle = Math.PI * 0.47;

const clock = new THREE.Clock();

/**
 * -------- Lighting (this is what makes it feel “premium”) ----------
 */
scene.add(new THREE.AmbientLight(0x7c90ff, 0.28));

const key = new THREE.DirectionalLight(0xbfd7ff, 1.0);
key.position.set(80, 120, 60);
key.castShadow = false;
scene.add(key);

const rim = new THREE.DirectionalLight(0xff7bd1, 0.55);
rim.position.set(-120, 60, -90);
scene.add(rim);

const groundGlow = new THREE.PointLight(0x4da3ff, 1.2, 260, 1.7);
groundGlow.position.set(0, 18, 0);
scene.add(groundGlow);

/**
 * -------- Ground + grid “streets” ----------
 */
const ground = new THREE.Mesh(
  new THREE.PlaneGeometry(600, 600, 1, 1),
  new THREE.MeshStandardMaterial({
    color: 0x070914,
    roughness: 0.95,
    metalness: 0.0,
    emissive: 0x01030a,
    emissiveIntensity: 1.0,
  })
);
ground.rotation.x = -Math.PI / 2;
scene.add(ground);

const grid = new THREE.GridHelper(600, 120, 0x17305e, 0x0b1326);
grid.position.y = 0.02;
grid.material.opacity = 0.25;
grid.material.transparent = true;
scene.add(grid);

/**
 * -------- Starfield ----------
 */
function addStarfield(count = 2000) {
  const geo = new THREE.BufferGeometry();
  const positions = new Float32Array(count * 3);
  for (let i = 0; i < count; i++) {
    const r = 260 + Math.random() * 400;
    const theta = Math.random() * Math.PI * 2;
    const y = 40 + Math.random() * 240;
    positions[i * 3 + 0] = Math.cos(theta) * r;
    positions[i * 3 + 1] = y;
    positions[i * 3 + 2] = Math.sin(theta) * r;
  }
  geo.setAttribute("position", new THREE.BufferAttribute(positions, 3));
  const mat = new THREE.PointsMaterial({ size: 0.75, opacity: 0.7, transparent: true });
  const pts = new THREE.Points(geo, mat);
  scene.add(pts);
  return pts;
}
const stars = addStarfield();

/**
 * -------- City generation ----------
 *
 * Each repo becomes a “tower cluster”:
 * - height: log(size_kb)
 * - emissive pulse: weekly commits
 * - color: language
 */
const city = new THREE.Group();
scene.add(city);

const items = repos
  .slice()
  // show the most recently updated first (visually)
  .sort((a, b) => (Date.parse(b.updated_at) || 0) - (Date.parse(a.updated_at) || 0));

const maxSizeKB = Math.max(...items.map((r) => r.size_kb || 0), 1);
const now = Date.now();

function activityFromWeekly(r) {
  const weeks = Array.isArray(r.weekly_commits_52w) ? r.weekly_commits_52w : [];
  if (weeks.length === 0) return { a: 0.08, pulse: 0.15 }; // fallback: still alive-ish
  const total = weeks.reduce((s, x) => s + (Number(x) || 0), 0);
  const recent = weeks.slice(-8).reduce((s, x) => s + (Number(x) || 0), 0);
  // normalize
  const a = clamp01(Math.log(1 + recent) / Math.log(1 + 60));
  const pulse = clamp01(Math.log(1 + total) / Math.log(1 + 400));
  return { a: 0.08 + a * 1.15, pulse: 0.15 + pulse * 1.0 };
}

function stalenessFactor(updatedAt) {
  const t = Date.parse(updatedAt);
  if (!Number.isFinite(t)) return 0.55;
  const days = (now - t) / (1000 * 60 * 60 * 24);
  // 0 days => 1.0, 180+ days => ~0.2
  return clamp01(1.0 - days / 220) * 0.9 + 0.1;
}

// Layout params
const cols = Math.ceil(Math.sqrt(items.length));
const spacing = 10.5;
const originX = -((cols - 1) * spacing) / 2;
const originZ = -((cols - 1) * spacing) / 2;

// Raycast interaction
const raycaster = new THREE.Raycaster();
const mouse = new THREE.Vector2();
let hovered = null;
let focused = null;

// Tooltip
const tooltip = document.getElementById("tooltip");

function setTooltip(repo, x, y) {
  if (!repo) {
    tooltip.style.display = "none";
    return;
  }
  tooltip.style.left = `${x}px`;
  tooltip.style.top = `${y}px`;

  const last = repo.last_commit_at ? new Date(repo.last_commit_at).toLocaleString() : "n/a";
  const upd = repo.updated_at ? new Date(repo.updated_at).toLocaleString() : "n/a";
  const lang = repo.language || "Unknown";

  tooltip.innerHTML = `
    <div class="name">${repo.full_name}</div>
    <div class="row"><span class="muted">Language</span><span>${lang}</span></div>
    <div class="row"><span class="muted">Size</span><span>${repo.size_readable}</span></div>
    <div class="row"><span class="muted">Updated</span><span>${upd}</span></div>
    <div class="row"><span class="muted">Last commit</span><span>${last}</span></div>
    <div style="margin-top:8px;font-size:12px;opacity:.9">
      <a href="${repo.html_url}" target="_blank" rel="noreferrer">Open on GitHub →</a>
    </div>
  `;
  tooltip.style.display = "block";
}

// Build meshes
const towerGeo = new THREE.BoxGeometry(1, 1, 1);
const pickables = [];

items.forEach((r, idx) => {
  const x = originX + (idx % cols) * spacing;
  const z = originZ + Math.floor(idx / cols) * spacing;

  const sizeNorm = Math.log(1 + r.size_kb) / Math.log(1 + maxSizeKB);
  const height = 2.0 + sizeNorm * 52.0;

  const color = colorForLanguage(r.language);
  const activity = activityFromWeekly(r);
  const stale = stalenessFactor(r.updated_at);

  // “district base”
  const base = new THREE.Mesh(
    new THREE.BoxGeometry(6.8, 0.6, 6.8),
    new THREE.MeshStandardMaterial({
      color: 0x0b1020,
      roughness: 0.85,
      metalness: 0.15,
      emissive: new THREE.Color(color),
      emissiveIntensity: 0.10 + stale * 0.15,
    })
  );
  base.position.set(x, 0.3, z);
  city.add(base);

  // Multiple towers per repo (makes it feel rich)
  const towers = new THREE.Group();
  towers.position.set(x, 0.0, z);

  const towerCount = 4 + Math.floor(6 * sizeNorm); // 4..10
  for (let i = 0; i < towerCount; i++) {
    const localX = (Math.random() - 0.5) * 5.2;
    const localZ = (Math.random() - 0.5) * 5.2;

    const h = height * (0.45 + Math.random() * 0.75);
    const w = 0.9 + Math.random() * 1.8;
    const d = 0.9 + Math.random() * 1.8;

    const mat = new THREE.MeshStandardMaterial({
      color: 0x0e1224,
      roughness: 0.35,
      metalness: 0.35,
      emissive: new THREE.Color(color),
      emissiveIntensity: (0.10 + activity.a * 0.55) * stale,
    });

    const m = new THREE.Mesh(towerGeo, mat);
    m.scale.set(w, h, d);
    m.position.set(localX, h / 2 + 0.6, localZ);

    // attach repo reference for picking
    m.userData.repo = r;
    m.userData.baseEmissive = mat.emissiveIntensity;
    m.userData.pulse = activity.pulse;
    m.userData.phase = Math.random() * Math.PI * 2;

    towers.add(m);
    pickables.push(m);
  }

  city.add(towers);

  // A subtle beacon for very active repos
  const weeks = Array.isArray(r.weekly_commits_52w) ? r.weekly_commits_52w : [];
  const recent = weeks.slice(-8).reduce((s, x) => s + (Number(x) || 0), 0);
  if (recent > 25) {
    const beacon = new THREE.PointLight(color, 1.1, 35, 2.2);
    beacon.position.set(x, 10 + height * 0.35, z);
    beacon.userData.repo = r;
    city.add(beacon);
  }
});

// Soft “skyline” silhouette
const skyline = new THREE.Mesh(
  new THREE.CylinderGeometry(260, 260, 40, 64, 1, true),
  new THREE.MeshStandardMaterial({
    color: 0x05060a,
    side: THREE.BackSide,
    emissive: 0x030417,
    emissiveIntensity: 0.8,
    roughness: 1.0,
    metalness: 0.0,
    transparent: true,
    opacity: 0.75,
  })
);
skyline.position.y = 15;
scene.add(skyline);

/**
 * -------- Interactions ----------
 */
function onMouseMove(e) {
  const rect = renderer.domElement.getBoundingClientRect();
  mouse.x = ((e.clientX - rect.left) / rect.width) * 2 - 1;
  mouse.y = -(((e.clientY - rect.top) / rect.height) * 2 - 1);

  // tooltip follows cursor if hovering
  if (hovered) setTooltip(hovered, e.clientX, e.clientY);
}
window.addEventListener("mousemove", onMouseMove);

window.addEventListener("click", (e) => {
  if (!hovered) return;

  focused = hovered;

  // Camera “snap focus” (smooth)
  const target = findRepoCenter(focused);
  if (target) {
    controls.target.copy(target);
    // pull camera closer but keep angle
    const dir = new THREE.Vector3().subVectors(camera.position, controls.target).normalize();
    camera.position.copy(controls.target).add(dir.multiplyScalar(75));
  }
});

function findRepoCenter(repo) {
  // Find one mesh that references this repo
  const m = pickables.find((p) => p.userData.repo?.full_name === repo.full_name);
  if (!m) return null;
  const wpos = new THREE.Vector3();
  m.getWorldPosition(wpos);
  // use ground-ish y
  return new THREE.Vector3(wpos.x, 10, wpos.z);
}

/**
 * -------- Render loop ----------
 */
function animate() {
  const t = clock.getElapsedTime();

  // subtle star drift
  stars.rotation.y = t * 0.02;

  // pulse emissive for “alive” feel
  for (const m of pickables) {
    const base = m.userData.baseEmissive ?? 0.2;
    const pulse = m.userData.pulse ?? 0.4;
    const phase = m.userData.phase ?? 0;
    const bump = (Math.sin(t * (0.9 + pulse * 2.0) + phase) * 0.5 + 0.5) * 0.35 * pulse;
    m.material.emissiveIntensity = base + bump;
  }

  // hover detection
  raycaster.setFromCamera(mouse, camera);
  const hits = raycaster.intersectObjects(pickables, false);

  const hitRepo = hits.length ? hits[0].object.userData.repo : null;
  if (hitRepo?.full_name !== hovered?.full_name) {
    hovered = hitRepo;
    if (!hovered) setTooltip(null);
    // subtle hover feedback: boost glow on hovered cluster meshes
    for (const m of pickables) {
      const isHover = hovered && m.userData.repo?.full_name === hovered.full_name;
      const base = m.userData.baseEmissive ?? 0.2;
      m.material.emissiveIntensity = isHover ? base * 1.45 : base;
    }
  }

  controls.update();
  renderer.render(scene, camera);
  requestAnimationFrame(animate);
}
animate();

/**
 * -------- Resize ----------
 */
addEventListener("resize", () => {
  camera.aspect = innerWidth / innerHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(innerWidth, innerHeight);
});
