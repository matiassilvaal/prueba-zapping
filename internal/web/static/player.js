(function () {
  'use strict';

  var SOURCE = '/stream/playlist.m3u8';
  var video = document.getElementById('video');
  var hls = null;

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

  window.ZappingPlayer = { start: startPlayer };
  startPlayer();
})();
