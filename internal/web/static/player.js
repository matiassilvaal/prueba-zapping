(function () {
  'use strict';

  var SOURCE = '/stream/playlist.m3u8';
  var video = document.getElementById('video');
  var els = {
    status: document.getElementById('status'),
    liveDot: document.getElementById('live-dot'),
    viewers: document.getElementById('viewers'),
    sequence: document.getElementById('sequence'),
    discSequence: document.getElementById('disc-sequence'),
    countdown: document.getElementById('countdown'),
    segments: document.getElementById('segments')
  };
  var hls = null;
  var lastSequence = -1;
  var nextTickLocal = null; // instante local (ms) del próximo tick

  // Arranca (o reinicia) la reproducción HLS con HLS.js o con soporte nativo
  function startPlayer() {
    if (window.Hls && Hls.isSupported()) {
      if (hls) { hls.destroy(); }
      hls = new Hls({ liveSyncDurationCount: 2 });
      hls.loadSource(SOURCE);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, function () { video.play().catch(function () {}); });
      hls.on(Hls.Events.ERROR, function (_, data) {
        if (!data.fatal) { return; }
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) { hls.startLoad(); }
        else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) { hls.recoverMediaError(); }
        else { startPlayer(); }
      });
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = SOURCE;
      video.play().catch(function () {});
    }
  }

  // Actualiza el estado de conexión en la barra superior
  function setStatus(text, live) {
    els.status.textContent = text;
    els.liveDot.classList.toggle('on', !!live);
  }

  // Pinta la ventana recibida por SSE
  function renderWindow(ev) {
    els.sequence.textContent = ev.sequence;
    els.discSequence.textContent = ev.discontinuitySequence;
    els.viewers.textContent = ev.viewers;
    nextTickLocal = Date.now() + ev.secondsToNextTick * 1000;
    els.segments.innerHTML = '';
    ev.segments.forEach(function (seg, i) {
      var li = document.createElement('li');
      if (i === 0) { li.classList.add('leaving'); }
      if (seg.discontinuity) { li.classList.add('discontinuity'); }
      var name = document.createElement('span');
      name.textContent = seg.name;
      var dur = document.createElement('span');
      dur.textContent = seg.duration.toFixed(3) + ' s';
      li.appendChild(name);
      li.appendChild(dur);
      els.segments.appendChild(li);
    });
  }

  // Cuenta regresiva local al próximo tick (4 veces por segundo)
  setInterval(function () {
    if (nextTickLocal === null) { return; }
    var remaining = Math.max(0, (nextTickLocal - Date.now()) / 1000);
    els.countdown.textContent = remaining.toFixed(1) + ' s';
  }, 250);

  // Conecta al canal SSE; EventSource reintenta solo al perder la conexión
  function connectEvents() {
    var es = new EventSource('/events');
    es.addEventListener('window', function (e) {
      var ev = JSON.parse(e.data);
      if (lastSequence >= 0 && ev.sequence < lastSequence) {
        // El servidor se reinició: la secuencia retrocedió, recargamos la fuente.
        startPlayer();
      }
      lastSequence = ev.sequence;
      renderWindow(ev);
    });
    es.addEventListener('viewers', function (e) {
      els.viewers.textContent = JSON.parse(e.data).viewers;
    });
    es.onopen = function () { setStatus('EN VIVO', true); };
    es.onerror = function () { setStatus('Reconectando…', false); };
  }

  window.ZappingPlayer = { start: startPlayer };
  startPlayer();
  connectEvents();
})();
