// SentinelDesk
// A collaborative operating system for people and AI agents.
//
// Copyright 2026 Federico Pereira <fpereira@cnsoluciones.com>
// Co-authored by Nicolas Pereira <npereira@cnsoluciones.com>
//
// Licensed under the Apache License, Version 2.0.
//
// This product's name and logo are trademarks of Federico Pereira and are not
// covered by the license above. See the README for the trademark policy.
//
// SPDX-License-Identifier: Apache-2.0


/* WebRTC virtual desktop client.
 *
 * Security model: the WebSocket is the single
 * door. The FIRST frame must be {type:"auth"} with credentials or a session
 * token; nothing else — not even the ICE config — is delivered before it is
 * validated. On success the server replies {type:"auth", ok:true, token},
 * then {type:"config"} and the WebRTC offer. The token lives in
 * sessionStorage so F5 and network blips reconnect without re-typing the
 * password, and closing the tab forgets it.
 */

import { init as initI18n, t, apply as applyI18n, languages, currentLanguage, setLanguage }
  from './i18n.js';

// The dictionaries load before anything else is wired up, so the first render
// already comes out in the right language instead of flashing English first.
await initI18n();

const video = document.getElementById('screen');
const statusBox = document.getElementById('status');
const statusText = document.getElementById('status-text');
const statsBox = document.getElementById('stats');
const loginBox = document.getElementById('login');
const loginBtn = document.getElementById('login-btn');
const audioBtn = document.getElementById('btn-audio');
const logoutBtn = document.getElementById('btn-logout');

// The icon is inline SVG with both states drawn in; a class decides which one
// shows. Writing into the element would wipe the label sitting next to it.
function setAudioMuted(muted) {
  audioBtn.classList.toggle('muted', muted);
  audioBtn.setAttribute('aria-pressed', String(!muted));
}

let pc = null;
let ws = null;
let inputChannel = null;
let statsTimer = null;
let cfg = { iceServers: [], remoteCursor: false };

let authRequired = false;
let sessionToken = sessionStorage.getItem('sentineldesk_token') || '';

/* The token is kept in sessionStorage only. It used to be mirrored into a
 * cookie as well, because the documentation was a page the browser NAVIGATED
 * to and a navigation carries no headers. The guide is on the project site now,
 * so nothing this server serves is reached by navigation any more, and the
 * cookie had no second reader. */
let pendingCreds = null;   // credentials taken from the login form
let stopRetrying = false;  // set on logout or auth failure

function setStatus(text) {
  statusText.textContent = text;
  statusBox.classList.remove('hidden');
}

function hideStatus() {
  statusBox.classList.add('hidden');
}

/* ---- Login --------------------------------------------------------------- */

function showLogin(errorText) {
  statusBox.classList.add('hidden');
  loginBox.classList.add('visible');
  document.getElementById('login-error').textContent = errorText || '';
  loginBtn.disabled = false;
  loginBtn.textContent = t('login.submit');
  document.getElementById('login-user').focus();
}

document.getElementById('login-form').addEventListener('submit', (e) => {
  e.preventDefault();
  loginBtn.disabled = true;
  loginBtn.textContent = t('login.submitting');
  pendingCreds = {
    user: document.getElementById('login-user').value,
    pass: document.getElementById('login-pass').value,
  };
  stopRetrying = false;
  loginBox.classList.remove('visible');
  connect();
});

logoutBtn.addEventListener('click', () => {
  sessionStorage.removeItem('sentineldesk_token');
  sessionToken = '';
  pendingCreds = null;
  stopRetrying = true;
  if (ws) ws.close();
  cleanup();
  showLogin();
});

/* ---- Connection ---------------------------------------------------------- */

async function connect() {
  setStatus(t('status.connecting'));
  try {
    const session = await (await fetch('/auth')).json();
    authRequired = !!session.required;
  } catch (err) {
    setStatus(t('status.unreachable'));
    setTimeout(connect, 3000);
    return;
  }
  logoutBtn.style.display = authRequired ? '' : 'none';

  if (authRequired && !sessionToken && !pendingCreds) {
    showLogin();
    return;
  }

  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  ws = new WebSocket(`${proto}://${location.host}/ws`);

  ws.onopen = () => {
    setStatus(t('status.authenticating'));
    // First frame: credentials or session token. Without a valid one the
    // server never starts the WebRTC handshake.
    const hello = { type: 'auth' };
    if (pendingCreds) { hello.user = pendingCreds.user; hello.pass = pendingCreds.pass; }
    else if (sessionToken) { hello.token = sessionToken; }
    ws.send(JSON.stringify(hello));
  };

  ws.onclose = () => {
    cleanup();
    if (stopRetrying) return;
    setStatus(t('status.disconnected'));
    setTimeout(connect, 3000);
  };

  ws.onmessage = async (e) => {
    const msg = JSON.parse(e.data);

    if (msg.type === 'auth') {
      if (msg.ok) {
        pendingCreds = null;
        if (msg.token) {
          sessionToken = msg.token;
          sessionStorage.setItem('sentineldesk_token', sessionToken);
        }
        setStatus(t('status.negotiating'));
      } else {
        sessionStorage.removeItem('sentineldesk_token');
              sessionToken = '';
        pendingCreds = null;
        stopRetrying = true;
        showLogin(t(msg.reason === 'locked' ? 'login.locked' : 'login.badCredentials'));
      }
      return;
    }

    if (msg.type === 'fatal') {
      // The server cannot give us a session and says why. Retrying will not
      // fix it, so stop and show the reason.
      stopRetrying = true;
      setStatus(msg.reason || 'The session could not be started');
      return;
    }

    if (msg.type === 'config') {
      cfg = { iceServers: msg.iceServers || [], remoteCursor: !!msg.remoteCursor };
      if (msg.version) {
        document.getElementById('about').textContent = 'SentinelDesk ' + msg.version;
      }
      applyCursor();
      return;
    }

    if (msg.type === 'offer') {
      // Only the FIRST offer builds the connection. The ones after it are
      // renegotiations (switching the microphone on) over the same one:
      // a new PeerConnection would cut the video and redo ICE from scratch.
      if (!pc) pc = createPeer(cfg);
      await pc.setRemoteDescription({ type: 'offer', sdp: msg.sdp });
      const answer = await pc.createAnswer();
      await pc.setLocalDescription(answer);
      ws.send(JSON.stringify({ type: 'answer', sdp: answer.sdp }));
    } else if (msg.type === 'ice' && pc) {
      try {
        await pc.addIceCandidate({
          candidate: msg.candidate,
          sdpMLineIndex: msg.sdpMLineIndex,
        });
      } catch (err) {
        console.warn('ICE candidate rejected:', err);
      }
    }
  };
}

function createPeer(cfg) {
  const peer = new RTCPeerConnection({ iceServers: cfg.iceServers });

  peer.ontrack = (e) => {
    // Minimal jitter buffer: we prioritize latency over smoothness.
    try {
      if ('jitterBufferTarget' in e.receiver) e.receiver.jitterBufferTarget = 0;
      if ('playoutDelayHint' in e.receiver) e.receiver.playoutDelayHint = 0;
    } catch (_) { /* browser-dependent */ }
    if (video.srcObject !== e.streams[0]) {
      video.srcObject = e.streams[0];
      // Audio enabled by default. If the browser's autoplay policy rejects
      // unmuted playback (no user gesture yet), fall back to muted and let
      // the first click on the desktop unmute.
      video.muted = false;
      setAudioMuted(false);
      video.play().catch(() => {
        video.muted = true;
        setAudioMuted(true);
        video.play().catch(() => {});
      });
    }
  };

  peer.onicecandidate = (e) => {
    if (e.candidate && ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({
        type: 'ice',
        candidate: e.candidate.candidate,
        sdpMLineIndex: e.candidate.sdpMLineIndex,
      }));
    }
  };

  peer.onconnectionstatechange = () => {
    if (peer.connectionState === 'connected') hideStatus();
    else if (peer.connectionState === 'failed') setStatus(t('status.webrtcFailed'));
    else if (peer.connectionState === 'connecting') setStatus(t('status.webrtcEstablishing'));
  };

  peer.ondatachannel = (e) => {
    if (e.channel.label === 'input') {
      inputChannel = e.channel;
      inputChannel.onopen = () => console.log('Input DataChannel ready');
      // The server pushes the real X pointer shape (resize arrows, text
      // beam, hand…) as a PNG; apply it as the CSS cursor with its hotspot.
      inputChannel.onmessage = (ev) => {
        try {
          const m = JSON.parse(ev.data);
          if (m.t === 'cursor' && !cfg.remoteCursor) {
            lastCursorCSS = m.d ? `url(${m.d}) ${m.x} ${m.y}, default` : 'default';
            applyCursor();
          } else if (m.t === 'clip') {
            // Something was copied on the remote desktop -> local clipboard.
            lastClip = m.d;
            navigator.clipboard.writeText(m.d).catch(() => {});
          } else if (m.t === 'presence') {
            applyPresence(m);
          } else if (m.t === 'peer_cursor') {
            // Nothing to do: peer pointers are drawn on the desktop itself
            // (internal/desktop/pointers.go), so they arrive inside the video.
            // Drawing them here as well showed every participant twice.
          } else if (m.t === 'download') {
            // The server finished a screenshot or recording and is sending it.
            // The URL carries a one-use ticket, spent by the download itself.
            const a = document.createElement('a');
            a.href = m.url; a.download = m.name || '';
            document.body.appendChild(a); a.click(); a.remove();
          } else if (m.t === 'control_request') {
            showControlRequest(m);
          } else if (m.t === 'control_request_done') {
            hideControlRequest();
          } else if (m.t === 'capture_state') {
            setServerRecording(!!m.recording);
          } else if (m.t === 'restreams') {
            applyRestreams(m);
          } else if (m.t === 'capture_error') {
            toast(m.error === 'needControl'
              ? t('media.needControl')
              : t('capture.failed', { error: m.error }), true);
            setServerRecording(false);
          } else if (m.t === 'upstream_error') {
            // The server could not pour the track in (typically a missing
            // Revert the button instead of claiming it is on.
            const btn = micBtn;
            const key = 'mic';
            if (upstream[key]) {
              upstream[key].getTracks().forEach((t) => t.stop());
              upstream[key] = null;
            }
            setMicLive(btn, false);
            toast(m.error, true);
          }
        } catch (_) { /* ignore non-JSON messages */ }
      };
      startGamepadLoop();
    }
  };

  return peer;
}

function cleanup() {
  if (pc) { pc.close(); pc = null; }
  inputChannel = null;
}

function sendInput(obj) {
  if (inputChannel && inputChannel.readyState === 'open') {
    inputChannel.send(JSON.stringify(obj));
  }
}

/* ---- The shared session ---------------------------------------------------
 *
 * Several people can watch the same desktop, but only one drives: otherwise
 * two mice fight and nobody understands what is happening. Whoever is watching
 * gets a button to ask for control, and each participant's pointer is drawn on
 * the desktop itself with their name, so it is clear who is pointing at what.
 */

const presenceBox = document.getElementById('presence');
const presenceText = document.getElementById('presence-text');
const controlBtn = document.getElementById('btn-control');
const presCount = document.getElementById('pres-count');
const presN = document.getElementById('pres-n');
const watchingBox = document.getElementById('watching');
const watchingMsg = watchingBox.querySelector('.msg');

let myMemberID = '';
let iHaveControl = true;   // alone in the room until told otherwise
let lastPresence = null;

// True when the desktop is drawing our marker: we are watching, and there is
// somebody to watch with. Mirrors the condition in Room.UpdatePointer.
let markerRepresentsMe = false;

/* The floating notice states whichever thing is true, and stays while it is.
 *
 * It was first written for one case — somebody else is driving, wait — and it
 * now covers the opposite one: nobody is driving, the controls are yours for
 * the asking. Both are conditions, not events, so both stay on screen until
 * they stop being true.
 *
 * A timed banner was the first attempt and it was wrong. The controls being
 * free is exactly when a person needs the button, and a notice that removes
 * itself after a few seconds is one you miss by looking away — which leaves
 * the toolbar as the only route, which is the problem it was meant to solve.
 */

function applyPresence(msg) {
  lastPresence = msg;
  myMemberID = msg.you || myMemberID;
  const members = msg.members || [];
  const me = members.find((m) => m.id === myMemberID);
  const controller = members.find((m) => m.controller);
  iHaveControl = !!(me && me.controller);

  markerRepresentsMe = !iHaveControl && members.length > 1;
  applyCursor();

  const solo = members.length <= 1;
  // Nobody at the controls is now a state of its own, and a common one: control
  // is claimed, never inherited, so a room can sit free while everyone watches.
  // It has to be told apart from "somebody else is driving" — the first invites
  // you to take over, the second asks you to wait.
  const nobodyDriving = !controller;
  // An agent in the room is worth saying out loud: a pointer that moves on its
  // own reads as a glitch until you know a model is driving.
  const agent = members.find((m) => m.agent);
  // The state lands on the rail as well as on the module: the 3px signature
  // strip and the collapsed ring both read from it, and they are what is left
  // once the labels are gone.
  for (const el of [presenceBox, toolbar]) {
    el.classList.toggle('solo', solo);
    el.classList.toggle('you-control', iHaveControl);
    el.classList.toggle('free', !iHaveControl && nobodyDriving);
    el.classList.toggle('watching', !iHaveControl && !nobodyDriving);
  }

  presCount.textContent = solo ? '' : t('room.connected', { n: members.length });
  presenceBox.classList.toggle('has-agent', !!agent);
  toolbar.classList.toggle('agent-controls', !!(agent && agent.controller));
  presN.textContent = solo ? '' : String(members.length);

  if (iHaveControl) {
    presenceText.textContent = t('room.youHaveControl');
    controlBtn.hidden = false;
    controlBtn.textContent = t('room.release');
    controlBtn.title = t('room.releaseHint');
  } else if (nobodyDriving) {
    presenceText.textContent = solo ? t('room.freeAlone') : t('room.free');
    controlBtn.hidden = false;
    controlBtn.textContent = t('room.take');
    controlBtn.title = t('room.freeHint');
  } else {
    presenceText.textContent = t('room.someoneHasControl', { who: controller.name });
    controlBtn.hidden = false;
    controlBtn.textContent = t('room.take');
    controlBtn.title = '';
  }

  // Two messages share one element and mean opposite things: "somebody else is
  // driving, wait" and "nobody is driving, go ahead". Only one can be true, and
  // whichever it is stands until it stops being true. Holding control silences
  // both — there is nothing to tell somebody who already has the desktop.
  const someoneElseDrives = !iHaveControl && !nobodyDriving;
  const upForGrabs = !iHaveControl && nobodyDriving;

  watchingBox.classList.toggle('free', upForGrabs);
  watchingBox.classList.toggle('show', someoneElseDrives || upForGrabs);
  if (someoneElseDrives) watchingMsg.innerHTML = t('room.watching');
  if (upForGrabs) watchingMsg.innerHTML = t('room.freeNotice');

  // The microphone travels upstream, and only the controller may publish. Say
  // so on the control rather than letting the click fail silently.
  micBtn.setAttribute('aria-disabled', String(!iHaveControl));
  if (!iHaveControl && upstream.mic) toggleUpstream('mic', micBtn, null);
}

function requestControlToggle(e) {
  // Give the keyboard straight back: a focused button would swallow the keys
  // that belong to the remote desktop.
  if (e && e.currentTarget) e.currentTarget.blur();
  sendInput({ t: iHaveControl ? 'release_control' : 'take_control' });
}
controlBtn.addEventListener('click', requestControlToggle);
document.getElementById('btn-control-mini').addEventListener('click', requestControlToggle);
document.getElementById('btn-take-watch').addEventListener('click', requestControlToggle);

// The video is drawn with object-fit:contain, so placing anything at the right
// spot means undoing that letterboxing first.
function videoToScreen(x, y) {
  const rect = video.getBoundingClientRect();
  const vw = video.videoWidth, vh = video.videoHeight;
  if (!vw || !vh) return null;
  const scale = Math.min(rect.width / vw, rect.height / vh);
  return {
    left: (rect.width - vw * scale) / 2 + x * scale,
    top: (rect.height - vh * scale) / 2 + y * scale,
  };
}

/* Which pointer you see, and when.
 *
 * The desktop draws a labelled marker for everyone who is NOT driving, and that
 * marker travels inside the video. So a viewer would otherwise see two pointers
 * of their own: the browser's native one, instant, and their own marker, one
 * video round-trip behind.
 *
 * While you are watching, the marker IS your pointer — the same thing everyone
 * else sees, and the same thing a recording keeps. Hiding the native one leaves
 * exactly one. The moment you take control the native cursor comes back, and
 * with it the immediacy you need in order to click accurately.
 */
let lastCursorCSS = 'default';

function applyCursor() {
  if (cfg.remoteCursor) { video.style.cursor = 'none'; return; }
  video.style.cursor = markerRepresentsMe ? 'none' : lastCursorCSS;
}

/* ---- The microphone, into the desktop -------------------------------------
 *
 * Audio always travelled outward; this is the way back. Whatever the browser
 * captures arrives inside as an ordinary capture device, so an application on
 * the desktop can use it the way it would use any microphone.
 *
 * The server's offer already declares the slot recvonly, so switching the
 * microphone on is a matter of replacing the track — no renegotiation needed.
 */

const micBtn = document.getElementById('btn-mic');
const micLiveChip = document.getElementById('mic-live');

// A pulsing LIVE capsule next to the label. An open microphone is easy to
// forget, and forgetting is the failure that matters.
function setMicLive(btn, on) {
  btn.classList.toggle('live', on);
  btn.setAttribute('aria-pressed', String(on));
  if (btn === micBtn) micLiveChip.hidden = !on;
}
const upstream = { mic: null };

// Finds the FREE upstream slot of the requested kind.
//
// The test matters: every transceiver has a receiver, so matching on
// `receiver.track.kind` also picks the one carrying the desktop's video — and
// setting THAT to `sendonly` kills the picture. What identifies an upstream
// slot is that it was negotiated as `inactive`: the server offered it
// `recvonly` and we had nothing to send yet. The one receiving the desktop
// sits at `recvonly`.
function transceiverFor(kind) {
  if (!pc) return null;
  return pc.getTransceivers().find(
    (t) => t.receiver && t.receiver.track && t.receiver.track.kind === kind &&
           t.sender && !t.sender.track &&
           (t.currentDirection === 'inactive' || t.currentDirection === null)) || null;
}

async function toggleUpstream(kind, btn, constraints) {
  if (!pc || pc.connectionState !== 'connected') {
    toast(t('media.notConnected'), true);
    return;
  }
  // Already running: shut it down.
  if (upstream[kind]) {
    const want = kind === 'mic' ? 'audio' : 'video';
    upstream[kind].getTracks().forEach((t) => t.stop());
    const tr = pc.getTransceivers().find(
      (t) => t.sender && t.sender.track && t.sender.track.kind === want);
    if (tr) {
      await tr.sender.replaceTrack(null);
      tr.direction = 'inactive';
      ws.send(JSON.stringify({ type: 'renegotiate' }));
    }
    upstream[kind] = null;
    setMicLive(btn, false);
    toast(t('media.micOff'));
    return;
  }
  if (!iHaveControl) {
    toast(t('media.needControl'), true);
    return;
  }
  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia(constraints);
  } catch (err) {
    // A refusal from the user, a busy device, or a page without HTTPS.
    toast(err.name === 'NotAllowedError'
      ? t('media.permissionDenied')
      : t('media.cannotOpen', { kind, error: err.message }), true);
    return;
  }
  const track = stream.getTracks()[0];
  const tr = transceiverFor(track.kind);
  if (!tr) {
    stream.getTracks().forEach((t) => t.stop());
    toast(t('media.noSlot'), true);
    return;
  }
  await tr.sender.replaceTrack(track);
  // When the session was negotiated there was nothing to send, so this m-line
  // was left `inactive`. replaceTrack alone does not revive it: the direction
  // has to be declared, and the server asked for a fresh offer.
  tr.direction = 'sendonly';
  ws.send(JSON.stringify({ type: 'renegotiate' }));

  upstream[kind] = stream;
  setMicLive(btn, true);
  toast(t('media.micOn'));
  // If the device disappears — unplugged — clear the state.
  track.addEventListener('ended', () => {
    upstream[kind] = null;
    setMicLive(btn, false);
  });
}

/* ---- Choosing WHICH microphone to share -----------------------------------
 *
 * Without this the browser takes its default device, which with several
 * microphones is rarely the one you want, so choosing is the whole point.
 *
 * A browser detail: device names are blank until permission has been granted
 * once. So when they come back empty, permission is requested with a throwaway
 * stream, that stream is closed, and only then can they be listed by name.
 */

const DEVICE_KEY = { mic: 'sentineldesk_mic_id' };

async function listDevices(type) {
  let devs = (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === type);
  if (devs.length && devs.every((d) => !d.label)) {
    // No permission yet: ask once to unlock the names.
    try {
      const probe = await navigator.mediaDevices.getUserMedia(
        type === 'audioinput' ? { audio: true } : { video: true });
      probe.getTracks().forEach((t) => t.stop());
      devs = (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === type);
    } catch (_) { /* refused: they are listed without names */ }
  }
  return devs;
}

// A floating menu under the button. Returns the chosen deviceId, or null if
// it is dismissed.
function chooseDevice(devs, btn, current) {
  return new Promise((resolve) => {
    document.querySelectorAll('.dev-menu').forEach((m) => m.remove());
    const menu = document.createElement('div');
    menu.className = 'dev-menu';
    const r = btn.getBoundingClientRect();
    menu.style.top = (r.bottom + 6) + 'px';
    menu.style.right = (window.innerWidth - r.right) + 'px';

    devs.forEach((d, i) => {
      const item = document.createElement('button');
      item.textContent = d.label || t('media.device', { n: i + 1 });
      if (d.deviceId === current) item.classList.add('current');
      item.addEventListener('click', () => { menu.remove(); resolve(d.deviceId); });
      menu.appendChild(item);
    });

    document.body.appendChild(menu);
    const away = (e) => {
      if (!menu.contains(e.target) && e.target !== btn) {
        menu.remove(); document.removeEventListener('mousedown', away); resolve(null);
      }
    };
    setTimeout(() => document.addEventListener('mousedown', away), 0);
  });
}

// Works out which device to use: with only one, that one; with several, it
// asks the first time and remembers the answer afterwards.
async function resolveDevice(kind, btn, forcePick) {
  const type = kind === 'mic' ? 'audioinput' : 'videoinput';
  let devs;
  try { devs = await listDevices(type); } catch (_) { return undefined; }
  if (!devs.length) return undefined;
  if (devs.length === 1 && !forcePick) return devs[0].deviceId;

  const saved = localStorage.getItem(DEVICE_KEY[kind]);
  if (saved && !forcePick && devs.some((d) => d.deviceId === saved)) return saved;

  const picked = await chooseDevice(devs, btn, saved);
  if (picked) localStorage.setItem(DEVICE_KEY[kind], picked);
  return picked || undefined;  // undefined = cancelado
}

function micConstraints(deviceId) {
  const audio = { echoCancellation: true, noiseSuppression: true };
  if (deviceId) audio.deviceId = { exact: deviceId };
  return { audio };
}

async function startUpstream(kind, btn, forcePick) {
  // Switching off needs no choice.
  if (upstream[kind]) return toggleUpstream(kind, btn, null);
  const id = await resolveDevice(kind, btn, forcePick);
  if (id === undefined && forcePick) return;   // the menu was dismissed
  const c = micConstraints(id);
  return toggleUpstream(kind, btn, c);
}

micBtn.addEventListener('click', () => startUpstream('mic', micBtn, false));

// Right-click on the button: pick the device again even when one is already
// remembered. It is the way out when another microphone is plugged in.
micBtn.addEventListener('contextmenu', (e) => { e.preventDefault(); pickAgain('mic', micBtn); });

async function pickAgain(kind, btn) {
  if (upstream[kind]) await toggleUpstream(kind, btn, null); // close the current one
  await startUpstream(kind, btn, true);
}

/* ---- Coordinate mapping (object-fit: contain adds letterboxing) ---------- */

function remoteCoords(e) {
  const rect = video.getBoundingClientRect();
  const vw = video.videoWidth, vh = video.videoHeight;
  if (!vw || !vh) return null;
  const scale = Math.min(rect.width / vw, rect.height / vh);
  const dw = vw * scale, dh = vh * scale;
  const ox = rect.left + (rect.width - dw) / 2;
  const oy = rect.top + (rect.height - dh) / 2;
  const x = Math.round((e.clientX - ox) / scale);
  const y = Math.round((e.clientY - oy) / scale);
  if (x < 0 || y < 0 || x >= vw || y >= vh) return null;
  return { x, y };
}

/* ---- Mouse --------------------------------------------------------------- */

let lastMove = 0;
video.addEventListener('mousemove', (e) => {
  const now = performance.now();
  if (now - lastMove < 1000 / 120) return; // up to 120 events/s
  lastMove = now;
  const p = remoteCoords(e);
  if (p) sendInput({ t: 'mm', x: p.x, y: p.y });
});

// Say something when somebody tries to use a desktop they are only watching.
//
// Without this the desktop just goes quiet: clicks do nothing and there is
// no way to know why. The notice appears on the attempt to interact, which is
// exactly when it matters, and offers to take control right there.
let lastViewerWarn = 0;
function warnIfWatching() {
  if (iHaveControl) return false;
  const now = Date.now();
  if (now - lastViewerWarn > 4000) {
    lastViewerWarn = now;
    toast(t('room.watching'), true);
    // Highlight the button so the eye finds it.
    controlBtn.animate(
      [{ transform: 'scale(1)' }, { transform: 'scale(1.18)' }, { transform: 'scale(1)' }],
      { duration: 500, iterations: 2 });
  }
  return true;
}

video.addEventListener('mousedown', (e) => {
  video.focus();
  if (warnIfWatching()) { e.preventDefault(); return; }
  // If autoplay forced us to start muted, the first click (a user gesture)
  // turns the audio on.
  if (video.muted) {
    video.muted = false;
    setAudioMuted(false);
  }
  const p = remoteCoords(e);
  if (!p) return;
  sendInput({ t: 'mm', x: p.x, y: p.y });
  sendInput({ t: 'mb', b: e.button === 1 ? 2 : e.button === 2 ? 3 : 1, d: 1 });
  e.preventDefault();
});

video.addEventListener('mouseup', (e) => {
  sendInput({ t: 'mb', b: e.button === 1 ? 2 : e.button === 2 ? 3 : 1, d: 0 });
  e.preventDefault();
});

video.addEventListener('contextmenu', (e) => e.preventDefault());

let wheelAccY = 0, wheelAccX = 0;
video.addEventListener('wheel', (e) => {
  e.preventDefault();
  wheelAccY += e.deltaY;
  wheelAccX += e.deltaX;
  const ty = Math.trunc(wheelAccY / 60);
  const tx = Math.trunc(wheelAccX / 60);
  if (ty || tx) {
    wheelAccY -= ty * 60;
    wheelAccX -= tx * 60;
    sendInput({ t: 'mw', dy: ty, dx: tx });
  }
}, { passive: false });

/* ---- Keyboard ------------------------------------------------------------- */

window.addEventListener('keydown', (e) => {
  if (loginBox.classList.contains('visible')) return;
  // With a window of this layer open the keys are its own, not the desktop's.
  if (document.getElementById('fm').classList.contains('open')) return;
  if (document.getElementById('rs').classList.contains('open')) {
    // Esc closes it; everything else belongs to the field being typed in.
    if (e.key === 'Escape') { closeRS(); e.preventDefault(); }
    return;
  }
  // º shows and hides the rail; it never reaches the remote desktop.
  if (isToolbarKey(e)) { toggleToolbar(); e.preventDefault(); return; }
  if (!inputChannel) return;
  if (warnIfWatching()) { e.preventDefault(); return; }
  sendInput({ t: 'kb', k: e.key, d: 1 });
  e.preventDefault();
});

window.addEventListener('keyup', (e) => {
  if (!inputChannel || loginBox.classList.contains('visible')) return;
  // With a window of this layer open the keys are its own, not the desktop's.
  if (document.getElementById('fm').classList.contains('open')) return;
  if (document.getElementById('rs').classList.contains('open')) return;
  // The º release must not leak either: the desktop would receive a
  // key that was never pressed, and some applications act on it anyway.
  if (isToolbarKey(e)) { e.preventDefault(); return; }
  sendInput({ t: 'kb', k: e.key, d: 0 });
  e.preventDefault();
});

window.addEventListener('blur', () => sendInput({ t: 'reset' }));

/* ---- Clipboard (bidirectional) ------------------------------------------- */

let lastClip = '';

// Local clipboard -> remote desktop. Reading requires focus + permission, so
// we sync whenever the window/tab regains focus (a paste in remote follows).
async function syncLocalClipboard() {
  if (!inputChannel || !navigator.clipboard || !navigator.clipboard.readText) return;
  try {
    const text = await navigator.clipboard.readText();
    if (text && text !== lastClip) {
      lastClip = text;
      sendInput({ t: 'clip', clip: text });
    }
  } catch (_) { /* not focused or permission denied */ }
}
window.addEventListener('focus', syncLocalClipboard);
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible') syncLocalClipboard();
});

/* ---- Gamepad (Gamepad API -> remote virtual joystick) -------------------- */

let gamepadRAF = null;
let lastGamepad = '';

function startGamepadLoop() {
  if (gamepadRAF !== null) return;
  const poll = () => {
    const pads = navigator.getGamepads ? navigator.getGamepads() : [];
    const gp = pads && [...pads].find((p) => p && p.connected);
    if (gp) {
      const buttons = gp.buttons.map((b) => (b.pressed ? 1 : b.value || 0));
      // Round axes to avoid flooding the channel with jitter.
      const axes = gp.axes.map((a) => Math.round(a * 100) / 100);
      const snapshot = JSON.stringify([buttons, axes]);
      if (snapshot !== lastGamepad) {
        lastGamepad = snapshot;
        sendInput({ t: 'gp', gb: buttons, ga: axes });
      }
    }
    gamepadRAF = requestAnimationFrame(poll);
  };
  gamepadRAF = requestAnimationFrame(poll);
}

window.addEventListener('gamepadconnected', () => {
  console.log('Gamepad connected');
  startGamepadLoop();
});

/* ---- The agent asking for control ------------------------------------------
 *
 * Between people, taking control is instant and needs no permission: everyone
 * arrived with the same credential and can see each other's names. An agent is
 * a different case — it acts faster than anybody can react — so it asks, and
 * somebody has to answer.
 *
 * Silence refuses. Nobody answering means nobody is looking, which is the worst
 * possible moment to let something start moving the mouse.
 */

const askBox = document.getElementById('ask-control');
let askID = 0;

function showControlRequest(m) {
  askID = m.id;
  askBox.querySelector('.ac-who').textContent = m.who || 'AI agent';
  askBox.querySelector('.ac-msg').innerHTML = t('ask.body', { who: m.who || 'AI agent' });
  const bar = askBox.querySelector('.ac-timer i');
  bar.style.animationDuration = (m.seconds || 45) + 's';
  // Restart the animation: without this a second request reuses the finished
  // one and the bar sits empty from the start.
  bar.style.animation = 'none';
  void bar.offsetWidth;
  bar.style.animation = '';
  bar.style.animationDuration = (m.seconds || 45) + 's';
  askBox.classList.add('show');
}

function hideControlRequest() {
  askBox.classList.remove('show');
  askID = 0;
}

function answerControl(grant) {
  if (!askID) return;
  sendInput({ t: 'control_answer', req: askID, grant });
  hideControlRequest();
}

document.getElementById('ask-allow').addEventListener('click', () => answerControl(true));
document.getElementById('ask-deny').addEventListener('click', () => answerControl(false));

/* ---- Popovers -------------------------------------------------------------
 *
 * Menus and readouts hang to the RIGHT of the rail rather than inside it: the
 * rail is 58px wide when collapsed and would have to grow to hold them, which
 * would make opening a menu shove the desktop sideways.
 *
 * Only one is open at a time. Two panels overlapping the desktop is one panel
 * too many when the desktop is the thing you are trying to see.
 */

// Statistics is deliberately NOT in here. A popover closes when attention moves
// on, which is the opposite of what a live readout is for: it exists to be
// watched WHILE using the desktop, so only its own button and its X dismiss it.
function closePopovers() {
  document.querySelectorAll('.sd-pop.floating').forEach((m) => m.remove());
  document.getElementById('btn-lang').setAttribute('aria-expanded', 'false');
}

// Anchored beside its button and clamped so it never runs off the bottom.
function placePopover(el, btn) {
  const r = btn.getBoundingClientRect();
  el.style.left = (r.right + 16) + 'px';
  el.style.top = Math.max(12, Math.min(window.innerHeight - el.offsetHeight - 12,
                                       r.top - 10)) + 'px';
}

/* ---- Toolbar -------------------------------------------------------------- */

audioBtn.addEventListener('click', (e) => {
  e.currentTarget.blur();
  video.muted = !video.muted;
  setAudioMuted(video.muted);
});

/* ---- Showing and hiding the rail -------------------------------------------
 *
 * The rail sits against the left edge and slides away with the º key, the way a
 * Quake console does. A slim whisker stays behind so nobody has to know the
 * shortcut exists.
 *
 * The key is matched by `code` as well as by `key`: on a Spanish layout that
 * key types º/ª and on an English one it types a backtick, but it is physically
 * the same one — top-left corner, where everybody expects a console.
 */

const toolbar = document.getElementById('toolbar');
const toolbarHandle = document.getElementById('toolbar-handle');

function isToolbarKey(e) {
  if (e.ctrlKey || e.altKey || e.metaKey) return false;
  // `code` is the physical key and survives every layout; the character
  // comparisons cover the layouts that put something else on that position.
  return e.code === 'Backquote' || e.key === 'º' || e.key === 'ª';
}

function setToolbar(visible) {
  toolbar.classList.toggle('hidden', !visible);
  toolbarHandle.hidden = visible;
  toolbarHandle.title = t('toolbar.show');
  if (!visible) closePopovers();
  try { localStorage.setItem('sentineldesk_toolbar', visible ? '1' : '0'); } catch (_) {}
}

function toggleToolbar() {
  setToolbar(toolbar.classList.contains('hidden'));
}

toolbarHandle.addEventListener('click', toggleToolbar);

// Collapse to icons only: the middle ground between the full rail and no rail
// at all. The tooltips still say what each button does, so what is lost is
// space, not information.
const collapseBtn = document.getElementById('toolbar-collapse');

function setCollapsed(on) {
  toolbar.classList.toggle('collapsed', on);
  collapseBtn.title = t(on ? 'toolbar.expand' : 'toolbar.collapse');
  closePopovers();
  try { localStorage.setItem('sentineldesk_toolbar_collapsed', on ? '1' : '0'); } catch (_) {}
}

collapseBtn.addEventListener('click', (e) => {
  e.currentTarget.blur();
  setCollapsed(!toolbar.classList.contains('collapsed'));
});
document.getElementById('toolbar-hide').addEventListener('click', (e) => {
  e.currentTarget.blur();
  setToolbar(false);
  toast(t('toolbar.hidden'));
});

setCollapsed((() => {
  try { return localStorage.getItem('sentineldesk_toolbar_collapsed') === '1'; } catch (_) { return false; }
})());

// Restore the rail's own visibility too, and with it the whisker: without this
// the handle sits over the desktop while the rail is already on screen.
setToolbar((() => {
  try { return localStorage.getItem('sentineldesk_toolbar') !== '0'; } catch (_) { return true; }
})());

/* ---- Screenshots and screen recording -------------------------------------
 *
 * The fallback path runs in the browser, and that is deliberate: the file has
 * to land on THE MACHINE OF WHOEVER IS WATCHING, not inside the container.
 * Since the stream is already here, capturing it client-side achieves that by
 * construction — no server disk, no server CPU, and nothing to go and fetch
 * over SFTP afterwards.
 *
 * The trade-off is worth knowing: this records what the client RECEIVED, with
 * the stream's compression and whatever the network did to it. For a clean copy
 * from the source there is the MCP's `start_recording`, which records inside
 * the container. They complement each other; they are not the same thing.
 */

const shotBtn = document.getElementById('btn-shot');
const flashEl = document.getElementById('flash');

// The way a camera confirms it fired: without it, a screenshot that lands on
// the host gives no sign anything happened.
function flashScreen() {
  flashEl.classList.add('on');
  setTimeout(() => flashEl.classList.remove('on'), 160);
}
const recBtn = document.getElementById('btn-rec');
const recBadge = document.getElementById('rec-badge');
const recTime = document.getElementById('rec-time');

let recorder = null;
let serverRecording = false;
let recTimer = null;
let recStarted = 0;

function stamp() {
  const d = new Date(), p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`;
}

// Downloads a Blob under the given name. This is the path that ends in the
// person's own downloads folder, which is exactly where it belongs.
function saveBlob(blob, name) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}

// The SERVER takes the screenshot: it comes from the original framebuffer,
// without the stream's compression, and arrives here over the same download
// channel the file manager uses. If the server cannot, this falls back to the
// browser's canvas.
shotBtn.addEventListener('click', (e) => {
  e.currentTarget.blur();
  flashScreen();
  if (!video.videoWidth) { toast(t('capture.noStream'), true); return; }
  if (inputChannel && inputChannel.readyState === 'open' && iHaveControl) {
    sendInput({ t: 'capture', action: 'shot' });
    return;
  }
  clientScreenshot();
});

function clientScreenshot() {
  try {
    // Captured at the desktop's native resolution, not the element's: shrinking
    // the image to fit the window would ruin the text.
    const cv = document.createElement('canvas');
    cv.width = video.videoWidth;
    cv.height = video.videoHeight;
    cv.getContext('2d').drawImage(video, 0, 0);
    cv.toBlob((blob) => {
      if (!blob) { toast(t('capture.failed', { error: 'canvas' }), true); return; }
      const name = `sentineldesk-${stamp()}.png`;
      saveBlob(blob, name);
      toast(t('capture.shotSaved', { name }));
    }, 'image/png');
  } catch (err) {
    toast(t('capture.failed', { error: err.message }), true);
  }
}

function pickRecorderMime() {
  // The first one the browser accepts. VP9 compresses better; VP8 is what
  // everybody understands; the last resort lets the browser decide.
  const wanted = [
    'video/webm;codecs=vp9,opus',
    'video/webm;codecs=vp8,opus',
    'video/webm',
  ];
  return wanted.find((m) => MediaRecorder.isTypeSupported(m)) || '';
}

function updateRecButton() {
  const on = !!recorder || serverRecording;
  // The icon holds both shapes and CSS picks one: replacing the element's text
  // would take the label with it.
  recBtn.classList.toggle('live', on);
  recBtn.setAttribute('aria-pressed', String(on));
  recBtn.title = t(on ? 'toolbar.recStop' : 'toolbar.rec');
  const lb = recBtn.querySelector('.lb');
  if (!on) { lb.textContent = t('label.rec'); return; }
  const secs = Math.floor((Date.now() - recStarted) / 1000);
  const time = Math.floor(secs / 60) + ':' + String(secs % 60).padStart(2, '0');
  lb.textContent = t('label.recStop', { time });
  recTime.textContent = time;
  document.getElementById('rec-mini').textContent = time;
}

async function startRecording() {
  const stream = video.srcObject;
  if (!stream || !video.videoWidth) { toast(t('capture.noStream'), true); return; }
  if (typeof MediaRecorder === 'undefined') { toast(t('capture.unsupported'), true); return; }

  const mimeType = pickRecorderMime();
  let rec;
  try {
    rec = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
  } catch (err) {
    toast(t('capture.failed', { error: err.message }), true);
    return;
  }

  const chunks = [];
  rec.ondataavailable = (e) => { if (e.data && e.data.size) chunks.push(e.data); };
  rec.onstop = () => {
    const blob = new Blob(chunks, { type: rec.mimeType || 'video/webm' });
    const name = `sentineldesk-${stamp()}.webm`;
    saveBlob(blob, name);
    toast(t('capture.recSaved', { name, size: humanSize(blob.size) }));
  };

  // One-second chunks: if the tab is closed abruptly that second is lost, not
  // the whole recording.
  rec.start(1000);
  recorder = rec;
  recStarted = Date.now();
  recBadge.hidden = false;
  recTimer = setInterval(updateRecButton, 500);
  updateRecButton();
  toast(t('capture.recStarted'));
}

function stopRecording() {
  if (!recorder) return;
  try { recorder.stop(); } catch (_) {}
  recorder = null;
  clearInterval(recTimer);
  recTimer = null;
  recBadge.hidden = true;
  updateRecButton();
}

// Recording also goes through the server, so the file comes out as MP4
// (H.264+AAC), which opens in QuickTime, on Windows and on a phone with nothing
// installed. The browser's MediaRecorder only produces WebM, which needs VLC.
recBtn.addEventListener('click', () => {
  const viaServer = inputChannel && inputChannel.readyState === 'open' && iHaveControl;
  if (serverRecording) { sendInput({ t: 'capture', action: 'rec_stop' }); return; }
  if (recorder) { stopRecording(); return; }
  if (viaServer) { sendInput({ t: 'capture', action: 'rec_start', format: 'mp4' }); return; }
  startRecording(); // fallback: WebM from the browser
});

// The server confirms it started or stopped; the button follows that state.
function setServerRecording(on) {
  serverRecording = on;
  if (on) {
    recStarted = Date.now();
    recBadge.hidden = false;
    recTimer = setInterval(updateRecButton, 500);
    toast(t('capture.recStarted'));
  } else {
    clearInterval(recTimer);
    recTimer = null;
    recBadge.hidden = true;
  }
  updateRecButton();
}

// If the connection drops mid-recording, close it and keep whatever
// haya en vez de perderlo.
window.addEventListener('beforeunload', stopRecording);

/* ---- Selector de idioma ----------------------------------------------------
 *
 * The server builds the list by enumerating assets/lang, so a new language
 * shows up here on its own. It reuses the same floating menu as the device
 * picker.
 */

const langBtn = document.getElementById('btn-lang');

langBtn.addEventListener('click', (e) => {
  e.currentTarget.blur();
  const open = langBtn.getAttribute('aria-expanded') === 'true';
  closePopovers();
  if (open) return;

  const menu = document.createElement('div');
  menu.className = 'sd-pop floating';
  menu.setAttribute('role', 'menu');
  menu.style.minWidth = '170px';

  languages().forEach((l) => {
    const item = document.createElement('button');
    item.setAttribute('role', 'menuitemradio');
    item.setAttribute('aria-checked', String(l.code === currentLanguage()));
    const name = document.createElement('span');
    name.textContent = l.name;
    item.appendChild(name);
    item.insertAdjacentHTML('beforeend',
      '<svg class="tick" viewBox="0 0 24 24"><path d="M4.5 12.5l5 5 10-11"/></svg>');
    item.addEventListener('click', async () => {
      closePopovers();
      await setLanguage(l.code);
    });
    menu.appendChild(item);
  });

  document.body.appendChild(menu);
  placePopover(menu, langBtn);
  langBtn.setAttribute('aria-expanded', 'true');

  const away = (ev) => {
    if (!menu.contains(ev.target) && !langBtn.contains(ev.target)) {
      closePopovers(); document.removeEventListener('mousedown', away);
    }
  };
  setTimeout(() => document.addEventListener('mousedown', away), 0);
});

document.getElementById('btn-fs').addEventListener('click', () => {
  if (document.fullscreenElement) document.exitFullscreen();
  else document.documentElement.requestFullscreen();
});

const statsBtn = document.getElementById('btn-stats');
const statsGrid = document.getElementById('stats-grid');
const statsSpark = document.getElementById('stats-spark');
const statsHistory = [];

function setStatsOpen(open) {
  statsBox.classList.toggle('show', open);
  statsBtn.setAttribute('aria-pressed', String(open));
  if (!open) {
    if (statsTimer) { clearInterval(statsTimer); statsTimer = null; }
    return;
  }
  if (statsTimer) return;
  startStats();
}

statsBtn.addEventListener('click', (e) => {
  e.currentTarget.blur();
  setStatsOpen(!statsBox.classList.contains('show'));
});
document.getElementById('stats-close').addEventListener('click', () => setStatsOpen(false));

/* Movable, like the file manager. Anchored bottom-left, so the drag is an
 * offset from that corner rather than absolute coordinates — the panel keeps
 * its place when the browser window is resized. */
const statsDrag = { x: 0, y: 0, active: false, startX: 0, startY: 0 };
const statsHead = statsBox.querySelector('.st-head');

statsHead.addEventListener('pointerdown', (e) => {
  if (e.target.closest('button')) return;
  statsDrag.active = true;
  statsDrag.startX = e.clientX - statsDrag.x;
  statsDrag.startY = e.clientY - statsDrag.y;
  statsHead.setPointerCapture(e.pointerId);
});
statsHead.addEventListener('pointermove', (e) => {
  if (!statsDrag.active) return;
  const r = statsBox.getBoundingClientRect();
  // Keep it inside: dragged past the edge there is no way to fetch it back.
  statsDrag.x = Math.max(-14, Math.min(window.innerWidth - r.width - 28, e.clientX - statsDrag.startX));
  statsDrag.y = Math.max(-(window.innerHeight - r.height - 28), Math.min(14, e.clientY - statsDrag.startY));
  statsBox.style.transform = `translate(${statsDrag.x}px, ${statsDrag.y}px)`;
});
for (const ev of ['pointerup', 'pointercancel']) {
  statsHead.addEventListener(ev, (e) => {
    if (!statsDrag.active) return;
    statsDrag.active = false;
    try { statsHead.releasePointerCapture(e.pointerId); } catch (_) {}
  });
}
statsHead.addEventListener('dblclick', (e) => {
  if (e.target.closest('button')) return;
  statsDrag.x = 0; statsDrag.y = 0;
  statsBox.style.transform = '';
});

function startStats() {
  let lastBytes = 0, lastTime = 0;
  const tick = async () => {
    if (!pc) return;
    const rows = { clock: '–', drift: '–', bitrate: '–', fps: '–',
                   latency: '–', loss: '–', packets: '–', frames: '–' };
    // A clock with milliseconds, so two browsers side by side can be compared
    // directly: if the numbers differ, they are not seeing the same instant.
    const now = new Date();
    rows.clock = now.toTimeString().slice(0, 8) + '.' +
                 String(now.getMilliseconds()).padStart(3, '0');
    let mbps = null;
    (await pc.getStats()).forEach((r) => {
      if (r.type === 'inbound-rtp' && r.kind === 'video') {
        if (lastTime) {
          mbps = (r.bytesReceived - lastBytes) * 8 / (r.timestamp - lastTime) / 1000;
          rows.bitrate = mbps.toFixed(1) + ' Mbps';
        }
        lastBytes = r.bytesReceived; lastTime = r.timestamp;
        rows.fps = r.framesPerSecond !== undefined ? String(r.framesPerSecond) : '–';
        rows.frames = String(r.framesDecoded || 0);
        rows.packets = String(r.packetsReceived || 0);
        // How far behind live this client is: the jitter buffer plus whatever
        // the decoder is holding. This is the number that differs when two
        // viewers drift apart, and the one worth watching.
        if (r.jitterBufferEmittedCount) {
          const jb = r.jitterBufferDelay / r.jitterBufferEmittedCount * 1000;
          rows.drift = Math.round(jb) + ' ms';
        }
        // Loss as a share of what was sent, not a raw count: 400 lost packets
        // means nothing without knowing whether that is out of a thousand or
        // out of a million.
        const total = (r.packetsReceived || 0) + (r.packetsLost || 0);
        if (total) rows.loss = ((r.packetsLost / total) * 100).toFixed(1) + ' %';
      }
      if (r.type === 'candidate-pair' && r.state === 'succeeded' &&
          r.currentRoundTripTime !== undefined) {
        rows.latency = (r.currentRoundTripTime * 1000).toFixed(0) + ' ms';
      }
    });

    statsGrid.innerHTML = '';
    for (const key of ['clock', 'drift', 'bitrate', 'fps', 'latency', 'loss', 'packets', 'frames']) {
      const k = document.createElement('span');
      k.className = 'k';
      k.textContent = t('stats.' + key);
      const v = document.createElement('span');
      v.textContent = rows[key];
      statsGrid.append(k, v);
    }

    // The sparkline is the reason to keep a window open at all: a single number
    // cannot tell a steady stream from one that is collapsing.
    if (mbps !== null) {
      statsHistory.push(mbps);
      if (statsHistory.length > 32) statsHistory.shift();
      const w = 184, h = 30;
      const top = Math.max(1, ...statsHistory);
      statsSpark.setAttribute('points', statsHistory.map((v, i) =>
        (i * (w / Math.max(1, statsHistory.length - 1))).toFixed(1) + ',' +
        (h - 2 - (v / top) * (h - 4)).toFixed(1)).join(' '));
    }
  };
  tick();
  statsTimer = setInterval(tick, 1000);
}

/* ---- File manager (Midnight Commander style) ------------------------------
 *
 * Two panes: the remote desktop on the left, the watcher's own machine on the
 * right. A browser cannot list the local disk by itself, so the right pane has
 * two modes:
 *
 *   - With the File System Access API (Chrome/Edge): the person picks a folder,
 *     it is listed for real side by side, and downloads are written into it.
 *   - Without it (Firefox/Safari): the pane shows the transfer queue and
 *     downloads go to the browser's own downloads folder.
 *
 * Downloads never carry the token in the URL: a one-use ticket is requested
 * (60 s, bound to that path) and only the ticket travels in the URL.
 */

const fm = {
  box: document.getElementById('fm'),
  win: document.getElementById('fm-window'),
  title: document.getElementById('fm-title'),
  remoteList: document.getElementById('remote-list'),
  localList: document.getElementById('local-list'),
  remoteCwd: document.getElementById('remote-cwd'),
  localCwd: document.getElementById('local-cwd'),
  remoteFoot: document.getElementById('remote-foot'),
  localFoot: document.getElementById('local-foot'),
  hint: document.getElementById('local-hint'),
  bar: document.querySelector('#fm-progress > div'),
  paneRemote: document.getElementById('pane-remote'),
  paneLocal: document.getElementById('pane-local'),
  path: '/',
  parent: '',
  entries: [],
  rows: [],
  cursor: 0,
  marked: new Set(),
  active: 'remote',
  localDir: null,      // FileSystemDirectoryHandle where the browser allows it
  localEntries: [],
  localCursor: 0,
  transfers: [],       // the visible queue when no local folder was chosen
};

function fmHeaders(extra) {
  const h = Object.assign({}, extra || {});
  if (sessionToken) h['X-SentinelDesk-Token'] = sessionToken;
  return h;
}

function humanSize(n) {
  if (n === undefined || n === null) return '';
  const u = ['B', 'K', 'M', 'G', 'T'];
  let i = 0, v = n;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return (i === 0 ? v : v.toFixed(v < 10 ? 1 : 0)) + u[i];
}

function shortDate(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d)) return '';
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function fmError(msg) {
  fm.remoteFoot.textContent = msg;
  fm.remoteFoot.style.color = '#ff8fa8';
  setTimeout(() => { fm.remoteFoot.style.color = ''; renderRemote(); }, 4000);
}

/* ---- panel remoto -------------------------------------------------------- */

async function loadRemote(path) {
  try {
    const res = await fetch(`/files/list?path=${encodeURIComponent(path || '/')}`,
                            { headers: fmHeaders() });
    const data = await res.json();
    if (!res.ok) { fmError(data.error || t('fm.errList', { error: res.status })); return; }
    fm.path = data.path;
    fm.parent = data.parent;
    fm.entries = data.entries || [];
    fm.cursor = 0;
    fm.marked.clear();
    fm.remoteCwd.textContent = data.root.replace(/\/$/, '') + data.path;
    renderRemote();
  } catch (err) {
    fmError(t('fm.errList', { error: err.message }));
  }
}

function renderRemote() {
  const ul = fm.remoteList;
  ul.innerHTML = '';
  const rows = [];
  if (fm.parent !== '') rows.push({ name: '..', type: 'up' });
  rows.push(...fm.entries);
  fm.rows = rows;

  rows.forEach((e, i) => {
    const li = document.createElement('li');
    li.className = (e.type === 'up' ? 'up' : e.type === 'dir' ? 'dir' : '');
    if (i === fm.cursor && fm.active === 'remote') li.classList.add('sel');
    if (fm.marked.has(e.name)) li.classList.add('marked');
    const nm = document.createElement('span');
    nm.className = 'nm';
    nm.textContent = e.type === 'up' ? '/..' : (e.type === 'dir' ? '/' : ' ') + e.name;
    const sz = document.createElement('span');
    sz.textContent = (e.type === 'dir' || e.type === 'up') ? '<DIR>' : humanSize(e.size);
    const md = document.createElement('span');
    md.textContent = shortDate(e.modified);
    li.append(nm, sz, md);
    li.addEventListener('click', (ev) => {
      fm.active = 'remote'; fm.cursor = i;
      if (ev.ctrlKey || ev.metaKey) toggleMark(e);
      renderRemote(); renderLocal();
    });
    li.addEventListener('dblclick', () => openRemote(e));
    ul.appendChild(li);
  });

  const dirs = fm.entries.filter((e) => e.type === 'dir').length;
  const bytes = fm.entries.reduce((a, e) => a + (e.type === 'dir' ? 0 : e.size), 0);
  fm.remoteFoot.textContent =
    t('fm.summary', { items: fm.entries.length, dirs, size: humanSize(bytes) }) +
    (fm.marked.size ? '  ·  ' + t('fm.marked', { n: fm.marked.size }) : '');
  document.getElementById('btn-download').disabled = selectedRemote().length === 0;
}

function toggleMark(e) {
  if (e.type === 'up') return;
  if (fm.marked.has(e.name)) fm.marked.delete(e.name);
  else fm.marked.add(e.name);
}

function remotePath(name) {
  return fm.path === '/' ? '/' + name : fm.path + '/' + name;
}

function openRemote(e) {
  if (e.type === 'up') return loadRemote(fm.parent);
  if (e.type === 'dir') return loadRemote(remotePath(e.name));
  downloadSelection();
}

/* ---- descarga ------------------------------------------------------------ */

function selectedRemote() {
  if (fm.marked.size) return fm.entries.filter((e) => fm.marked.has(e.name));
  const cur = fm.rows[fm.cursor];
  return cur && cur.type !== 'up' ? [cur] : [];
}

async function downloadSelection() {
  const items = selectedRemote();
  if (!items.length) return;
  for (const it of items) {
    try {
      const res = await fetch('/files/ticket', {
        method: 'POST',
        headers: fmHeaders({ 'Content-Type': 'application/json' }),
        body: JSON.stringify({ path: remotePath(it.name) }),
      });
      const data = await res.json();
      if (!res.ok) { fmError(data.error || t('fm.errTicket')); continue; }
      const url = `/files/download?t=${encodeURIComponent(data.ticket)}`;
      const suggested = it.type === 'dir' ? it.name + '.tar.gz' : it.name;

      if (fm.localDir) {
        await streamIntoLocalDir(url, suggested);
        fm.transfers.unshift({ name: suggested, size: it.size, status: t('fm.saved') });
      } else {
        const a = document.createElement('a');
        a.href = url;
        a.download = suggested;
        document.body.appendChild(a);
        a.click();
        a.remove();
        fm.transfers.unshift({ name: suggested, size: it.size, status: t('fm.downloaded') });
      }
      renderLocal();
    } catch (err) {
      fmError(t('fm.errDownload', { error: err.message }));
    }
  }
  fm.marked.clear();
  renderRemote();
}

async function streamIntoLocalDir(url, name) {
  const res = await fetch(url);
  if (!res.ok) { fmError(t('fm.errDownload', { error: res.status })); return; }
  const total = Number(res.headers.get('Content-Length') || 0);
  const handle = await fm.localDir.getFileHandle(name, { create: true });
  const writable = await handle.createWritable();
  const reader = res.body.getReader();
  let got = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    await writable.write(value);
    got += value.length;
    if (total) fm.bar.style.width = (got / total * 100) + '%';
  }
  await writable.close();
  fm.bar.style.width = '0';
  await loadLocal();
}

/* ---- subida -------------------------------------------------------------- */

async function uploadFiles(fileList) {
  if (!fileList || !fileList.length) return;
  const form = new FormData();
  [...fileList].forEach((f, i) => form.append('f' + i, f, f.name));
  fm.localFoot.textContent = t('fm.uploading', { n: fileList.length });
  try {
    const res = await fetch(`/files/upload?path=${encodeURIComponent(fm.path)}`, {
      method: 'POST', headers: fmHeaders(), body: form,
    });
    const data = await res.json();
    if (!res.ok) { fmError(data.error || t('fm.errUpload', { error: '' })); return; }
    [...fileList].forEach((f) =>
      fm.transfers.unshift({ name: f.name, size: f.size, status: t('fm.uploaded') }));
    await loadRemote(fm.path);
    renderLocal();
  } catch (err) {
    fmError(t('fm.errUpload', { error: err.message }));
  }
}

/* ---- panel local --------------------------------------------------------- */

async function loadLocal() {
  if (!fm.localDir) { renderLocal(); return; }
  const out = [];
  for await (const [name, handle] of fm.localDir.entries()) {
    if (handle.kind === 'directory') { out.push({ name, type: 'dir' }); continue; }
    try {
      const f = await handle.getFile();
      out.push({ name, type: 'file', size: f.size, modified: new Date(f.lastModified).toISOString() });
    } catch (_) { out.push({ name, type: 'file' }); }
  }
  out.sort((a, b) => (a.type === b.type
    ? (a.name.toLowerCase() < b.name.toLowerCase() ? -1 : 1)
    : (a.type === 'dir' ? -1 : 1)));
  fm.localEntries = out;
  fm.localCursor = Math.min(fm.localCursor, Math.max(0, out.length - 1));
  renderLocal();
}

function renderLocal() {
  const ul = fm.localList;
  ul.innerHTML = '';

  if (fm.localDir) {
    fm.hint.style.display = 'none';
    fm.localCwd.textContent = fm.localDir.name + '/';
    fm.localEntries.forEach((e, i) => {
      const li = document.createElement('li');
      li.className = e.type === 'dir' ? 'dir' : '';
      if (i === fm.localCursor && fm.active === 'local') li.classList.add('sel');
      const nm = document.createElement('span');
      nm.className = 'nm';
      nm.textContent = (e.type === 'dir' ? '/' : ' ') + e.name;
      const sz = document.createElement('span');
      sz.textContent = e.type === 'dir' ? '<DIR>' : humanSize(e.size);
      const st = document.createElement('span');
      st.textContent = shortDate(e.modified);
      li.append(nm, sz, st);
      li.addEventListener('click', () => {
        fm.active = 'local'; fm.localCursor = i; renderLocal(); renderRemote();
      });
      li.addEventListener('dblclick', () => uploadFromLocal(e));
      ul.appendChild(li);
    });
    const bytes = fm.localEntries.reduce((a, e) => a + (e.size || 0), 0);
    fm.localFoot.textContent = t('fm.localSummary',
      { items: fm.localEntries.length, size: humanSize(bytes) });
    return;
  }

  // With no folder chosen the pane is the transfer queue: a browser cannot
  // enumerate the disk unless the person picks a folder.
  fm.hint.style.display = '';
  fm.localCwd.textContent = t('fm.downloadsFolder');
  if (!fm.transfers.length) {
    const li = document.createElement('li');
    li.className = 'err';
    li.innerHTML = '<span class="nm"></span><span></span><span></span>';
    li.querySelector('.nm').textContent = t('fm.noTransfers');
    ul.appendChild(li);
  }
  fm.transfers.slice(0, 200).forEach((t) => {
    const li = document.createElement('li');
    const nm = document.createElement('span');
    nm.className = 'nm';
    nm.textContent = ' ' + t.name;
    const sz = document.createElement('span');
    sz.textContent = humanSize(t.size);
    const st = document.createElement('span');
    st.textContent = t.status;
    li.append(nm, sz, st);
    ul.appendChild(li);
  });
  fm.localFoot.textContent = t('fm.transfers', { n: fm.transfers.length });
}

async function uploadFromLocal(entry) {
  if (!fm.localDir || !entry || entry.type === 'dir') return;
  const handle = await fm.localDir.getFileHandle(entry.name);
  await uploadFiles([await handle.getFile()]);
}

document.getElementById('btn-pick').addEventListener('click', async () => {
  if (!window.showDirectoryPicker) return;
  try {
    fm.localDir = await window.showDirectoryPicker({ mode: 'readwrite' });
    await loadLocal();
  } catch (_) { /* dismissed by the person */ }
});

/* ---- acciones ------------------------------------------------------------ */

document.getElementById('btn-download').addEventListener('click', downloadSelection);

document.getElementById('btn-upload').addEventListener('click', async () => {
  if (fm.localDir && fm.localEntries[fm.localCursor]) {
    return uploadFromLocal(fm.localEntries[fm.localCursor]);
  }
  // With no folder chosen, the system picker is the source.
  const input = document.createElement('input');
  input.type = 'file';
  input.multiple = true;
  input.addEventListener('change', () => uploadFiles(input.files));
  input.click();
});

document.getElementById('key-refresh').addEventListener('click', () => loadRemote(fm.path));
document.getElementById('key-copy').addEventListener('click', downloadSelection);

document.getElementById('key-mkdir').addEventListener('click', async () => {
  const name = prompt(t('fm.promptMkdir'));
  if (!name) return;
  await fileOp('mkdir', remotePath(name));
});

document.getElementById('key-delete').addEventListener('click', async () => {
  const items = selectedRemote();
  if (!items.length) return;
  const what = items.length === 1 ? items[0].name : t('fm.nItems', { n: items.length });
  if (!confirm(t('fm.confirmDelete', { what }))) return;
  for (const it of items) await fileOp('delete', remotePath(it.name), null, true);
  loadRemote(fm.path);
});

document.getElementById('key-rename').addEventListener('click', async () => {
  const items = selectedRemote();
  if (items.length !== 1) return;
  const name = prompt(t('fm.promptRename'), items[0].name);
  if (!name || name === items[0].name) return;
  await fileOp('rename', remotePath(items[0].name), remotePath(name));
});

async function fileOp(op, path, to, quiet) {
  try {
    const res = await fetch('/files/op', {
      method: 'POST',
      headers: fmHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ op, path, to }),
    });
    const data = await res.json();
    if (!res.ok) { fmError(data.error || op + ': ' + res.status); return false; }
    if (!quiet) loadRemote(fm.path);
    return true;
  } catch (err) {
    fmError(op + ': ' + err.message);
    return false;
  }
}

/* ---- open / close / keyboard --------------------------------------------- */

function openFM() {
  fm.box.classList.add('open');
  fm.box.setAttribute('aria-hidden', 'false');
  // Without the directory API, hide the button that would do nothing.
  document.getElementById('local-pick-wrap').style.display =
    window.showDirectoryPicker ? '' : 'none';
  loadRemote(fm.path);
  renderLocal();
  fm.paneRemote.focus();
}

function closeFM() {
  fm.box.classList.remove('open');
  fm.box.setAttribute('aria-hidden', 'true');
  video.focus();
}

document.getElementById('btn-files').addEventListener('click', openFM);
document.getElementById('fm-close').addEventListener('click', closeFM);
document.getElementById('key-quit').addEventListener('click', closeFM);
/* ---- Moving the window ----------------------------------------------------
 *
 * The manager is centred by flexbox, so dragging works as an offset from that
 * centre rather than by switching to absolute coordinates: nothing has to be
 * recomputed when the browser window is resized.
 *
 * The offset is clamped so at least the title bar stays reachable. A window
 * dragged fully off-screen cannot be brought back — there is no taskbar here to
 * rescue it from.
 */
const fmDrag = { x: 0, y: 0, active: false, startX: 0, startY: 0 };

function applyFMOffset() {
  fm.win.style.transform = `translate(${fmDrag.x}px, ${fmDrag.y}px)`;
}

fm.title.addEventListener('pointerdown', (e) => {
  // The close button lives in this strip and is not a drag handle.
  if (e.target.closest('button')) return;
  fmDrag.active = true;
  fmDrag.startX = e.clientX - fmDrag.x;
  fmDrag.startY = e.clientY - fmDrag.y;
  fm.win.classList.add('dragging');
  fm.title.setPointerCapture(e.pointerId);
});

fm.title.addEventListener('pointermove', (e) => {
  if (!fmDrag.active) return;
  const r = fm.win.getBoundingClientRect();
  // Half the window may leave on each side; the title bar cannot leave the top.
  const maxX = (window.innerWidth + r.width) / 2 - 60;
  const maxY = (window.innerHeight + r.height) / 2 - 40;
  const minY = -((window.innerHeight - r.height) / 2);
  fmDrag.x = Math.max(-maxX, Math.min(maxX, e.clientX - fmDrag.startX));
  fmDrag.y = Math.max(minY, Math.min(maxY, e.clientY - fmDrag.startY));
  applyFMOffset();
});

for (const ev of ['pointerup', 'pointercancel']) {
  fm.title.addEventListener(ev, (e) => {
    if (!fmDrag.active) return;
    fmDrag.active = false;
    fm.win.classList.remove('dragging');
    try { fm.title.releasePointerCapture(e.pointerId); } catch (_) {}
  });
}

// Double-clicking the title bar puts it back in the middle, for when it ended
// up somewhere awkward.
fm.title.addEventListener('dblclick', (e) => {
  if (e.target.closest('button')) return;
  fmDrag.x = 0; fmDrag.y = 0;
  applyFMOffset();
});

// In the capture phase: otherwise the desktop's global listener takes the keys
// injecting them into the remote machine while the manager is being used.
window.addEventListener('keydown', (e) => {
  if (!fm.box.classList.contains('open')) return;
  const localMode = fm.active === 'local';
  const len = localMode ? fm.localEntries.length : fm.rows.length;
  const cur = localMode ? fm.localCursor : fm.cursor;
  const move = (n) => {
    const v = Math.max(0, Math.min(len - 1, n));
    if (localMode) fm.localCursor = v; else fm.cursor = v;
  };
  let handled = true;

  switch (e.key) {
    case 'Escape': closeFM(); break;
    case 'ArrowDown': move(cur + 1); break;
    case 'ArrowUp': move(cur - 1); break;
    case 'PageDown': move(cur + 12); break;
    case 'PageUp': move(cur - 12); break;
    case 'Home': move(0); break;
    case 'End': move(len - 1); break;
    case 'Tab': fm.active = localMode ? 'remote' : 'local'; break;
    case 'Enter':
      if (localMode) uploadFromLocal(fm.localEntries[cur]);
      else if (fm.rows[cur]) openRemote(fm.rows[cur]);
      break;
    case 'Backspace': if (!localMode && fm.parent !== '') loadRemote(fm.parent); break;
    case 'Insert':
    case ' ':
      if (!localMode && fm.rows[cur]) { toggleMark(fm.rows[cur]); move(cur + 1); }
      break;
    case 'F2': loadRemote(fm.path); break;
    case 'F5': downloadSelection(); break;
    case 'F6': document.getElementById('key-rename').click(); break;
    case 'F7': document.getElementById('key-mkdir').click(); break;
    case 'F8': document.getElementById('key-delete').click(); break;
    default: handled = false;
  }
  if (handled) {
    e.preventDefault();
    e.stopPropagation();
    renderRemote();
    renderLocal();
    const list = localMode ? fm.localList : fm.remoteList;
    const sel = list.querySelector('.sel');
    if (sel) sel.scrollIntoView({ block: 'nearest' });
  }
}, true);

/* ---- Dropping files onto the page -----------------------------------------
 *
 * Dragging a file from your own machine onto the tab and having it appear on
 * the remote desktop is the gesture people expect; making it work only inside
 * the file manager was a trap.
 *
 * The default destination is the remote user's Desktop folder, so the file
 * shows up as an icon rather than buried in the home directory. When the file
 * manager is open the directory being browsed wins instead: there the intent is
 * explicit.
 */

const dropzone = document.getElementById('dropzone');
const toastBox = document.getElementById('toast');
let dragDepth = 0;          // nested enter/leave: a counter, not a boolean
let desktopDir = null;      // '/Desktop' when it exists on the remote desktop

function toast(msg, isError) {
  toastBox.innerHTML = msg;
  toastBox.classList.toggle('err', !!isError);
  toastBox.classList.add('show');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => toastBox.classList.remove('show'), isError ? 6000 : 4000);
}

// Work out once whether a Desktop folder exists; otherwise use the home.
async function resolveDesktopDir() {
  if (desktopDir !== null) return desktopDir;
  desktopDir = '/';
  try {
    const res = await fetch('/files/list?path=/', { headers: fmHeaders() });
    if (res.ok) {
      const data = await res.json();
      const hit = (data.entries || []).find(
        (e) => e.type === 'dir' && /^Desktop$/i.test(e.name));
      if (hit) desktopDir = '/' + hit.name;
    }
  } catch (_) { /* stays at the home directory */ }
  document.getElementById('dz-target').textContent = '~' + desktopDir;
  return desktopDir;
}

function dropTarget() {
  // With the manager open, the directory being browsed wins.
  if (fm.box.classList.contains('open')) return fm.path;
  return desktopDir || '/';
}

// Only arms for real files: dragging text or a link inside the page has no
// business covering the desktop with an overlay.
function hasFiles(e) {
  const dt = e.dataTransfer;
  return !!dt && [...(dt.types || [])].includes('Files');
}

window.addEventListener('dragenter', (e) => {
  if (!hasFiles(e) || !sessionToken && authRequired) return;
  e.preventDefault();
  dragDepth++;
  resolveDesktopDir();
  dropzone.classList.add('armed');
});

window.addEventListener('dragover', (e) => {
  if (!hasFiles(e)) return;
  e.preventDefault();
  e.dataTransfer.dropEffect = 'copy';
});

window.addEventListener('dragleave', (e) => {
  if (!hasFiles(e)) return;
  dragDepth = Math.max(0, dragDepth - 1);
  if (dragDepth === 0) dropzone.classList.remove('armed');
});

window.addEventListener('drop', async (e) => {
  if (!hasFiles(e)) return;
  e.preventDefault();
  dragDepth = 0;
  dropzone.classList.remove('armed');
  const files = [...(e.dataTransfer.files || [])];
  if (!files.length) return;

  const target = await resolveDesktopDir().then(dropTarget);
  const total = files.reduce((a, f) => a + f.size, 0);
  toast(t('drop.copying', { n: files.length, size: humanSize(total) }));

  const form = new FormData();
  files.forEach((f, i) => form.append('f' + i, f, f.name));
  try {
    const res = await fetch(`/files/upload?path=${encodeURIComponent(target)}`, {
      method: 'POST', headers: fmHeaders(), body: form,
    });
    const data = await res.json();
    if (!res.ok) { toast(data.error || t('drop.failed'), true); return; }
    const names = (data.uploaded || []).map((u) => u.name);
    toast(t('drop.copied', { names: names.join(', '), path: target }));
    files.forEach((f) =>
      fm.transfers.unshift({ name: f.name, size: f.size, status: t('fm.uploaded') }));
    // If the manager is open on that directory, refresh it.
    if (fm.box.classList.contains('open') && fm.path === target) loadRemote(fm.path);
    renderLocal();
  } catch (err) {
    toast(t('drop.failed') + ': ' + err.message, true);
  }
});

// The browser opens the file in the tab unless the event is cancelled; with
// the listeners above, but the desktop still must not react to it.
video.addEventListener('dragover', (e) => e.preventDefault());

// Dropping onto the local pane uploads to the current remote directory.
// stopPropagation on all four: inside the pane its own highlight wins,
// not the whole-page overlay, which would send the file somewhere else.
['dragenter', 'dragover'].forEach((ev) =>
  fm.paneLocal.addEventListener(ev, (e) => {
    e.preventDefault(); e.stopPropagation(); fm.paneLocal.classList.add('drop');
  }));
['dragleave', 'drop'].forEach((ev) =>
  fm.paneLocal.addEventListener(ev, (e) => {
    e.preventDefault(); e.stopPropagation(); fm.paneLocal.classList.remove('drop');
  }));
fm.paneLocal.addEventListener('drop', (e) => {
  if (e.dataTransfer && e.dataTransfer.files.length) {
    // Stop propagation: otherwise the page-wide handler would also upload
    // the same files a second time.
    e.stopPropagation();
    uploadFiles(e.dataTransfer.files);
  }
});

/* ---- Streaming out --------------------------------------------------------
 *
 * The same encoded picture the room is already receiving, forwarded to one more
 * place. Nothing here starts a second capture, which is why going live does not
 * interrupt what everyone is watching.
 *
 * The panel behaves like the other windows of this layer: it survives clicks on
 * the rail, it moves, and only the X closes it.
 */
const rs = {
  box: document.getElementById('rs'),
  win: document.getElementById('rs-window'),
  title: document.getElementById('rs-title'),
  url: document.getElementById('rs-url'),
  label: document.getElementById('rs-label'),
  hint: document.getElementById('rs-hint'),
  kfWrap: document.getElementById('rs-kf-wrap'),
  kfNote: document.getElementById('rs-kf-note'),
  kf: document.getElementById('rs-kf'),
  audio: document.getElementById('rs-audio'),
  go: document.getElementById('rs-go'),
  msg: document.getElementById('rs-msg'),
  active: document.getElementById('rs-active'),
  list: document.getElementById('rs-list'),
  btn: document.getElementById('btn-rtmp'),
  chip: document.getElementById('rtmp-live'),
  platform: 'custom',
};

/* Where each platform takes a stream, and what it calls the thing you paste.
 *
 * The ingest addresses are the platforms' published ones and they are stable;
 * keeping them here means nobody has to go and look one up while deciding to
 * go live. What the person supplies is only ever the key. */
const RS_PLATFORMS = {
  youtube:  { ingest: 'rtmp://a.rtmp.youtube.com/live2/',        key: true },
  twitch:   { ingest: 'rtmp://live.twitch.tv/app/',              key: true },
  facebook: { ingest: 'rtmps://live-api-s.facebook.com:443/rtmp/', key: true },
  custom:   { ingest: '',                                        key: false },
};

/* What was typed last time, per destination.
 *
 * The server cannot help here: it redacts the stream key before the list ever
 * leaves the machine, so reopening the panel mid-stream would otherwise show
 * you dots instead of what you entered. This is also the only reason stopping
 * and restarting does not mean pasting the key again.
 *
 * A platform key IS a broadcast credential, so it is stored the same way a
 * browser stores a password: on this machine, for this origin, and nowhere
 * else. It never travels anywhere it was not already going.
 */
const RS_STORE = 'sentineldesk_restream';

function loadAddresses() {
  try { return JSON.parse(localStorage.getItem(RS_STORE)) || {}; }
  catch (_) { return {}; }
}

function rememberAddress(platform, value) {
  try {
    const all = loadAddresses();
    if (value) all[platform] = value; else delete all[platform];
    localStorage.setItem(RS_STORE, JSON.stringify(all));
  } catch (_) { /* private mode: the field simply starts empty next time */ }
}

function selectPlatform(name) {
  rs.platform = name;
  for (const b of document.querySelectorAll('.rs-plat')) {
    b.setAttribute('aria-checked', String(b.dataset.plat === name));
  }
  const spec = RS_PLATFORMS[name];
  rs.label.textContent = t(spec.key ? 'rs.keyLabel' : 'rs.urlLabel');
  rs.hint.textContent = t(spec.key ? 'rs.hintPlatform' : 'rs.hintCustom');
  rs.url.placeholder = spec.key ? t('rs.keyPlaceholder') : 'udp://host.docker.internal:5000';
  // The platforms are not asked whether viewers arrive mid-stream: they do, and
  // the server forces keyframes for them regardless. Only a receiver you point
  // at yourself gets the choice.
  rs.kfWrap.hidden = spec.key;
  rs.kfNote.hidden = spec.key;
  rs.url.value = loadAddresses()[name] || '';
  setRSMessage('');
}

function setRSMessage(text, bad) {
  rs.msg.textContent = text || '';
  rs.msg.classList.toggle('bad', !!bad);
}

function openRS() {
  rs.box.classList.add('open');
  rs.box.setAttribute('aria-hidden', 'false');
  rs.btn.setAttribute('aria-expanded', 'true');
  // Reopening mid-stream should show what is being sent, and the server's copy
  // of that is redacted on purpose. This is where it comes back from.
  rs.url.value = loadAddresses()[rs.platform] || '';
  sendInput({ t: 'restream', rs: { action: 'list' } });
  rs.url.focus();
  rs.url.select();
}

function closeRS() {
  rs.box.classList.remove('open');
  rs.box.setAttribute('aria-hidden', 'true');
  rs.btn.setAttribute('aria-expanded', 'false');
  video.focus();
}

for (const b of document.querySelectorAll('.rs-plat')) {
  b.addEventListener('click', () => selectPlatform(b.dataset.plat));
}
rs.btn.addEventListener('click', (e) => {
  e.currentTarget.blur();
  if (rs.box.classList.contains('open')) closeRS(); else openRS();
});
document.getElementById('rs-close').addEventListener('click', closeRS);

rs.go.addEventListener('click', () => {
  const spec = RS_PLATFORMS[rs.platform];
  const typed = rs.url.value.trim();
  if (!typed) {
    setRSMessage(t(spec.key ? 'rs.needKey' : 'rs.needUrl'), true);
    rs.url.focus();
    return;
  }
  // A platform gets its published ingest plus the key; anything else is taken
  // exactly as typed, because it is somebody's own address and we do not know
  // better than they do.
  const url = spec.key ? spec.ingest + typed : typed;
  // Kept as typed, not as sent: what goes back into the field next time has to
  // be the key, never the key with the ingest address glued in front of it.
  rememberAddress(rs.platform, typed);
  setRSMessage(t('rs.connecting'));
  rs.go.disabled = true;
  sendInput({
    t: 'restream',
    rs: {
      action: 'start', platform: rs.platform, url,
      audio: rs.audio.checked,
      kf: spec.key ? true : rs.kf.checked,
    },
  });
});

// Enter in the field is the same as pressing the button: the form has one
// field and one action.
rs.url.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') { e.preventDefault(); rs.go.click(); }
});

/* Applies whatever the server says is running. The list is authoritative — a
 * destination can also stop on its own, when a key is rejected or a receiver
 * goes away, and this is how that becomes visible. */
function applyRestreams(m) {
  rs.go.disabled = false;
  const list = Array.isArray(m.list) ? m.list : [];

  rs.list.replaceChildren();
  for (const d of list) {
    const li = document.createElement('li');
    const dest = document.createElement('span');
    dest.className = 'dest';
    const plat = document.createElement('span');
    plat.className = 'plat';
    plat.textContent = d.platform || 'custom';
    const url = document.createElement('span');
    url.className = 'url';
    // Already redacted by the server: the stream key is a credential and it
    // does not travel whole to every browser in the room.
    url.textContent = ' ' + d.url;
    dest.append(plat, url);

    const stop = document.createElement('button');
    stop.textContent = t('rs.stop');
    stop.addEventListener('click', () => {
      sendInput({ t: 'restream', rs: { action: 'stop', id: d.id } });
    });
    li.append(dest, stop);
    rs.list.append(li);
  }
  rs.active.hidden = list.length === 0;

  // The rail carries the badge, so a session being broadcast is visible without
  // opening anything.
  rs.btn.classList.toggle('live', list.length > 0);
  rs.chip.hidden = list.length === 0;

  if (m.error) {
    setRSMessage(m.error === 'needControl' ? t('media.needControl') : m.error, true);
  } else if (list.length) {
    setRSMessage(t('rs.live'));
  } else {
    setRSMessage('');
  }
  if (m.able === false) {
    rs.go.disabled = true;
    setRSMessage(t('rs.unavailable'), true);
  }
}

/* ---- Moving the panel, on the file manager's model ---------------------- */
const rsDrag = { x: 0, y: 0, active: false, startX: 0, startY: 0 };

rs.title.addEventListener('pointerdown', (e) => {
  if (e.target.closest('button')) return;
  rsDrag.active = true;
  rsDrag.startX = e.clientX - rsDrag.x;
  rsDrag.startY = e.clientY - rsDrag.y;
  rs.win.classList.add('dragging');
  rs.title.setPointerCapture(e.pointerId);
});
rs.title.addEventListener('pointermove', (e) => {
  if (!rsDrag.active) return;
  const r = rs.win.getBoundingClientRect();
  const maxX = (window.innerWidth + r.width) / 2 - 60;
  const maxY = (window.innerHeight + r.height) / 2 - 40;
  const minY = -((window.innerHeight - r.height) / 2);
  rsDrag.x = Math.max(-maxX, Math.min(maxX, e.clientX - rsDrag.startX));
  rsDrag.y = Math.max(minY, Math.min(maxY, e.clientY - rsDrag.startY));
  rs.win.style.transform = `translate(${rsDrag.x}px, ${rsDrag.y}px)`;
});
for (const ev of ['pointerup', 'pointercancel']) {
  rs.title.addEventListener(ev, (e) => {
    if (!rsDrag.active) return;
    rsDrag.active = false;
    rs.win.classList.remove('dragging');
    try { rs.title.releasePointerCapture(e.pointerId); } catch (_) {}
  });
}
rs.title.addEventListener('dblclick', (e) => {
  if (e.target.closest('button')) return;
  rsDrag.x = 0; rsDrag.y = 0;
  rs.win.style.transform = '';
});

// The label and the hint depend on which destination is chosen, so a language
// change has to go back through the same decision rather than through the
// static translation pass.
document.addEventListener('languagechange', () => selectPlatform(rs.platform));

/* ---- Documentation --------------------------------------------------------
 *
 * Opened in the VIEWER's browser, not inside the virtual desktop. Half of what
 * it explains is how to operate this rail, and reading that through the video
 * stream would mean taking control just to scroll it; the other half is
 * commands to run on your own machine, which is the side of the screen the
 * reader is already on.
 *
 * The language rides along, so the docs open in the same one the toolbar is in.
 */
/* The guide is published on the project site, not embedded in this binary.
 *
 * That is a deliberate trade and it is worth naming: the documentation now
 * needs a route to the internet, which nothing else here does. What it buys is
 * a single copy — one that can be corrected without cutting a release, and that
 * cannot drift from the version a reader is looking at, because there is only
 * ever one.
 *
 * Everything else in this binary stays self-contained. This is the one
 * dependency, and it is on documentation, not on operation: the desktop runs,
 * streams and takes instructions with no network beyond its own clients. */
const DOCS_URL = 'https://lordbasex.github.io/sentineldesk/docs/guide/index.html';

function docsURL(hash) {
  return DOCS_URL + '?lang=' + encodeURIComponent(currentLanguage()) + (hash || '');
}

function openDocs(hash) {
  window.open(docsURL(hash), 'sentineldesk-docs', 'noopener');
}

document.getElementById('btn-help').addEventListener('click', (e) => {
  e.currentTarget.blur();
  openDocs();
});

const rsHelp = document.getElementById('rs-help');
rsHelp.addEventListener('click', (e) => {
  e.preventDefault();
  openDocs('#capture-stream-out');
});

selectPlatform('custom');

/* ---- Debug handle ---------------------------------------------------------
 *
 * Moving to an ES module took the variables above out of global scope — which
 * is correct, but leaves the browser console with nothing to look at. This
 * exposes just enough for live diagnosis and for automated tests. It is the
 * client's own page: nothing here was not already inspectable.
 */
window.sentineldesk = {
  get pc() { return pc; },
  get connected() { return !!pc && pc.connectionState === 'connected'; },
  get videoSize() { return video.videoWidth + 'x' + video.videoHeight; },
  get hasControl() { return iHaveControl; },
  get language() { return currentLanguage(); },
  languages,
  setLanguage,
  sendInput,
  t,
};

connect();
