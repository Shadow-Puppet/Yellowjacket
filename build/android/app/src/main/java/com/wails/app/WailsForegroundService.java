package com.wails.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.ServiceInfo;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.media.AudioAttributes;
import android.media.AudioFocusRequest;
import android.media.AudioManager;
import android.media.MediaMetadata;
import android.media.session.MediaSession;
import android.media.session.PlaybackState;
import android.os.Build;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.util.Log;

import androidx.annotation.Nullable;

import org.json.JSONObject;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * The foreground service that keeps playback alive with the screen off, and
 * the app's whole media-control surface: a {@link MediaSession} for the lock
 * screen and headset buttons, a transport notification, and audio focus.
 *
 * <p>The scaffold shipped this as a generic "keep the process alive" service
 * typed {@code dataSync}. YellowJacket's reason for staying alive in the
 * background is that a song is playing, so it is {@code mediaPlayback} — the
 * manifest and {@code startForeground} must agree on that or the call throws.
 *
 * <p>It is driven entirely from Go. {@code backend/mediacontrols/android.go}
 * pushes a JSON payload through
 * {@link WailsBridge#startForegroundService(String)}, and every command the
 * user gives here — a notification button, the lock screen, a headset, or the
 * OS taking audio focus away — goes back the other way as a
 * {@code yj:media:command} event. Nothing about playback is decided here: this
 * class renders state and reports intent.
 */
public class WailsForegroundService extends android.app.Service {
    public static final String ACTION_START = "com.wails.app.FGS_START";

    // Transport actions, delivered to ourselves by the notification's
    // PendingIntents. getService rather than a broadcast: a receiver would
    // have to be exported or registered, and this service is already the
    // thing that has to be running for any of them to be meaningful.
    private static final String ACTION_PLAY = "com.wails.app.MEDIA_PLAY";
    private static final String ACTION_PAUSE = "com.wails.app.MEDIA_PAUSE";
    private static final String ACTION_NEXT = "com.wails.app.MEDIA_NEXT";
    private static final String ACTION_PREVIOUS = "com.wails.app.MEDIA_PREVIOUS";

    private static final String TAG = "WailsMedia";
    private static final String CHANNEL_ID = "yellowjacket_playback";
    private static final int NOTIFICATION_ID = 0x57A1; // "WAI"
    private static final String COMMAND_EVENT = "yj:media:command";

    /** Cover art is decoded down to this, which is larger than any lock screen. */
    private static final int ART_MAX_PX = 512;

    /**
     * Whether an instance is alive. {@link WailsBridge} reads it to decide
     * between startForegroundService and startService: from Android 12 an app
     * in the background may not <em>start</em> a foreground service, but it
     * may go on delivering intents to one it already has — and every update
     * after the first (a track change with the screen off, most of them) is
     * exactly that case.
     */
    static volatile boolean running = false;

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final ExecutorService artExecutor = Executors.newSingleThreadExecutor();

    private MediaSession session;
    private AudioManager audioManager;
    private AudioFocusRequest focusRequest; // API 26+ only.
    private AudioManager.OnAudioFocusChangeListener focusListener;

    private String title = "";
    private String artist = "";
    private String album = "";
    private String artPath = "";
    private long durationMs = 0;
    private long positionMs = 0;
    private boolean playing = false;

    private Bitmap art;

    private boolean hasFocus = false;
    /**
     * Whether *we* paused because focus went away. Only then does regaining it
     * resume: a user who paused during a phone call did not ask us to start
     * again when it ended.
     */
    private boolean pausedByFocusLoss = false;

    private boolean noisyRegistered = false;

    /** Headphones pulled out. Anything else and the room hears the album. */
    private final BroadcastReceiver noisyReceiver = new BroadcastReceiver() {
        @Override
        public void onReceive(Context context, Intent intent) {
            if (AudioManager.ACTION_AUDIO_BECOMING_NOISY.equals(intent.getAction())) {
                emitCommand("pause");
            }
        }
    };

    @Override
    public void onCreate() {
        super.onCreate();
        running = true;
        audioManager = (AudioManager) getSystemService(AUDIO_SERVICE);
        createChannel();
        createSession();
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? null : intent.getAction();

        if (ACTION_PLAY.equals(action)) {
            emitCommand("play");
        } else if (ACTION_PAUSE.equals(action)) {
            emitCommand("pause");
        } else if (ACTION_NEXT.equals(action)) {
            emitCommand("next");
        } else if (ACTION_PREVIOUS.equals(action)) {
            emitCommand("previous");
        } else if (intent != null) {
            applyPayload(intent);
        }

        // Unconditionally, on every path: a service started with
        // startForegroundService that returns from onStartCommand without
        // calling startForeground is killed with a
        // ForegroundServiceDidNotStartInTimeException.
        goForeground();

        return START_STICKY;
    }

    /**
     * Read the state Go pushed. "payload" is the whole JSON document; the
     * title/text extras are the scaffold's original contract and are kept as a
     * fallback so a non-media caller still gets a sensible notification.
     */
    private void applyPayload(Intent intent) {
        String payload = intent.getStringExtra("payload");
        if (payload == null || payload.isEmpty()) {
            if (intent.getStringExtra("title") != null) {
                title = intent.getStringExtra("title");
            }
            if (intent.getStringExtra("text") != null) {
                artist = intent.getStringExtra("text");
            }
            return;
        }

        try {
            JSONObject o = new JSONObject(payload);
            title = o.optString("title", "");
            artist = o.optString("artist", "");
            album = o.optString("album", "");
            durationMs = o.optLong("durationSec", 0) * 1000L;
            positionMs = o.optLong("positionSec", 0) * 1000L;
            playing = "playing".equals(o.optString("state", "paused"));

            String path = o.optString("artPath", "");
            if (!path.equals(artPath)) {
                artPath = path;
                loadArt(path);
            }
        } catch (Exception e) {
            Log.e(TAG, "bad media payload", e);
            return;
        }

        if (playing) {
            requestFocus();
            registerNoisy();
        } else {
            unregisterNoisy();
        }

        updateSession();
    }

    // --- MediaSession ------------------------------------------------------

    private void createSession() {
        session = new MediaSession(this, "YellowJacket");
        session.setFlags(MediaSession.FLAG_HANDLES_MEDIA_BUTTONS
                | MediaSession.FLAG_HANDLES_TRANSPORT_CONTROLS);
        session.setCallback(new MediaSession.Callback() {
            @Override
            public void onPlay() {
                emitCommand("play");
            }

            @Override
            public void onPause() {
                emitCommand("pause");
            }

            @Override
            public void onStop() {
                emitCommand("stop");
            }

            @Override
            public void onSkipToNext() {
                emitCommand("next");
            }

            @Override
            public void onSkipToPrevious() {
                emitCommand("previous");
            }

            @Override
            public void onSeekTo(long pos) {
                try {
                    JSONObject o = new JSONObject();
                    o.put("command", "seek");
                    o.put("positionSec", pos / 1000L);
                    WailsBridge.emitFromService(COMMAND_EVENT, o.toString());
                } catch (Exception e) {
                    Log.e(TAG, "seek command failed", e);
                }
            }
        });
        session.setActive(true);
    }

    private void updateSession() {
        MediaMetadata.Builder meta = new MediaMetadata.Builder()
                .putString(MediaMetadata.METADATA_KEY_TITLE, title)
                .putString(MediaMetadata.METADATA_KEY_ARTIST, artist)
                .putString(MediaMetadata.METADATA_KEY_ALBUM, album)
                .putLong(MediaMetadata.METADATA_KEY_DURATION, durationMs);
        if (art != null) {
            meta.putBitmap(MediaMetadata.METADATA_KEY_ALBUM_ART, art);
        }
        session.setMetadata(meta.build());

        // The position is an anchor, not a clock: the state carries the
        // playback speed and the OS interpolates from here, which is why the
        // Go side only pushes on a real state change or a seek.
        PlaybackState state = new PlaybackState.Builder()
                .setActions(PlaybackState.ACTION_PLAY
                        | PlaybackState.ACTION_PAUSE
                        | PlaybackState.ACTION_PLAY_PAUSE
                        | PlaybackState.ACTION_STOP
                        | PlaybackState.ACTION_SKIP_TO_NEXT
                        | PlaybackState.ACTION_SKIP_TO_PREVIOUS
                        | PlaybackState.ACTION_SEEK_TO)
                .setState(playing ? PlaybackState.STATE_PLAYING : PlaybackState.STATE_PAUSED,
                        positionMs, playing ? 1.0f : 0.0f)
                .build();
        session.setPlaybackState(state);
    }

    // --- Notification ------------------------------------------------------

    private void createChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return;
        }
        NotificationManager nm = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        // LOW: a transport notification is a control surface, not news, and
        // IMPORTANCE_DEFAULT would make a sound on every track change.
        NotificationChannel ch = new NotificationChannel(
                CHANNEL_ID, "Playback", NotificationManager.IMPORTANCE_LOW);
        ch.setShowBadge(false);
        nm.createNotificationChannel(ch);
    }

    private void goForeground() {
        Notification n = buildNotification();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIFICATION_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK);
        } else {
            startForeground(NOTIFICATION_ID, n);
        }
    }

    @SuppressWarnings("deprecation")
    private Notification buildNotification() {
        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                ? new Notification.Builder(this, CHANNEL_ID)
                : new Notification.Builder(this);

        b.setSmallIcon(android.R.drawable.ic_media_play)
                .setContentTitle(title.isEmpty() ? getString(R.string.app_name) : title)
                .setContentText(artist)
                .setSubText(album)
                .setOngoing(playing)
                .setVisibility(Notification.VISIBILITY_PUBLIC)
                .setContentIntent(launchIntent());

        if (art != null) {
            b.setLargeIcon(art);
        }

        b.addAction(new Notification.Action.Builder(
                android.R.drawable.ic_media_previous, "Previous",
                transportIntent(ACTION_PREVIOUS, 1)).build());
        b.addAction(playing
                ? new Notification.Action.Builder(android.R.drawable.ic_media_pause, "Pause",
                        transportIntent(ACTION_PAUSE, 2)).build()
                : new Notification.Action.Builder(android.R.drawable.ic_media_play, "Play",
                        transportIntent(ACTION_PLAY, 3)).build());
        b.addAction(new Notification.Action.Builder(
                android.R.drawable.ic_media_next, "Next",
                transportIntent(ACTION_NEXT, 4)).build());

        Notification.MediaStyle style = new Notification.MediaStyle()
                .setShowActionsInCompactView(0, 1, 2);
        if (session != null) {
            style.setMediaSession(session.getSessionToken());
        }
        b.setStyle(style);

        return b.build();
    }

    private PendingIntent transportIntent(String action, int requestCode) {
        Intent i = new Intent(this, WailsForegroundService.class).setAction(action);
        return PendingIntent.getService(this, requestCode, i, pendingIntentFlags());
    }

    private PendingIntent launchIntent() {
        Intent launch = getPackageManager().getLaunchIntentForPackage(getPackageName());
        if (launch == null) {
            return null;
        }
        return PendingIntent.getActivity(this, 0, launch, pendingIntentFlags());
    }

    private int pendingIntentFlags() {
        // Mandatory from S, unavailable before M.
        return Build.VERSION.SDK_INT >= Build.VERSION_CODES.M
                ? PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT
                : PendingIntent.FLAG_UPDATE_CURRENT;
    }

    // --- Cover art ---------------------------------------------------------

    /**
     * Decode the cover off the main thread and redraw when it lands. A track
     * change must not wait on a JPEG, and the notification is correct without
     * one — it simply has no image until this returns.
     */
    private void loadArt(final String path) {
        art = null;
        if (path == null || path.isEmpty()) {
            return;
        }

        artExecutor.execute(() -> {
            Bitmap decoded = decodeScaled(path);
            mainHandler.post(() -> {
                // The track may have changed while we decoded.
                if (!path.equals(artPath)) {
                    return;
                }
                art = decoded;
                updateSession();
                goForeground();
            });
        });
    }

    private Bitmap decodeScaled(String path) {
        try {
            BitmapFactory.Options bounds = new BitmapFactory.Options();
            bounds.inJustDecodeBounds = true;
            BitmapFactory.decodeFile(path, bounds);

            int longest = Math.max(bounds.outWidth, bounds.outHeight);
            int sample = 1;
            while (longest / sample > ART_MAX_PX) {
                sample *= 2;
            }

            BitmapFactory.Options opts = new BitmapFactory.Options();
            opts.inSampleSize = sample;
            return BitmapFactory.decodeFile(path, opts);
        } catch (Throwable t) {
            // OutOfMemoryError included: a missing cover is not a crash.
            Log.w(TAG, "cover art decode failed: " + path, t);
            return null;
        }
    }

    // --- Audio focus -------------------------------------------------------

    private void requestFocus() {
        if (hasFocus || audioManager == null) {
            return;
        }

        if (focusListener == null) {
            focusListener = this::onFocusChange;
        }

        int result;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            AudioAttributes attrs = new AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_MEDIA)
                    .setContentType(AudioAttributes.CONTENT_TYPE_MUSIC)
                    .build();
            // No setWillPauseWhenDucked: from Oreo the framework ducks us
            // itself and reports no CAN_DUCK loss, so the Go-side duck below
            // is a pre-Oreo path. Asking to be told instead would mean
            // pausing for every notification tone.
            focusRequest = new AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
                    .setAudioAttributes(attrs)
                    .setOnAudioFocusChangeListener(focusListener, mainHandler)
                    .build();
            result = audioManager.requestAudioFocus(focusRequest);
        } else {
            result = requestFocusLegacy();
        }

        hasFocus = result == AudioManager.AUDIOFOCUS_REQUEST_GRANTED;
    }

    @SuppressWarnings("deprecation")
    private int requestFocusLegacy() {
        return audioManager.requestAudioFocus(focusListener,
                AudioManager.STREAM_MUSIC, AudioManager.AUDIOFOCUS_GAIN);
    }

    @SuppressWarnings("deprecation")
    private void abandonFocus() {
        if (!hasFocus || audioManager == null) {
            return;
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && focusRequest != null) {
            audioManager.abandonAudioFocusRequest(focusRequest);
        } else {
            audioManager.abandonAudioFocus(focusListener);
        }
        hasFocus = false;
    }

    private void onFocusChange(int change) {
        switch (change) {
            case AudioManager.AUDIOFOCUS_LOSS:
                // Someone else owns the output now, for good.
                hasFocus = false;
                pausedByFocusLoss = false;
                emitCommand("pause");
                break;
            case AudioManager.AUDIOFOCUS_LOSS_TRANSIENT:
                // A phone call. Remember that the pause was ours to undo.
                pausedByFocusLoss = playing;
                emitCommand("pause");
                break;
            case AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK:
                emitDuck(true);
                break;
            case AudioManager.AUDIOFOCUS_GAIN:
                hasFocus = true;
                emitDuck(false);
                if (pausedByFocusLoss) {
                    pausedByFocusLoss = false;
                    emitCommand("play");
                }
                break;
            default:
                break;
        }
    }

    // --- Noisy (headphones) ------------------------------------------------

    private void registerNoisy() {
        if (noisyRegistered) {
            return;
        }
        registerReceiver(noisyReceiver,
                new IntentFilter(AudioManager.ACTION_AUDIO_BECOMING_NOISY));
        noisyRegistered = true;
    }

    private void unregisterNoisy() {
        if (!noisyRegistered) {
            return;
        }
        try {
            unregisterReceiver(noisyReceiver);
        } catch (IllegalArgumentException ignored) {
            // Already gone; nothing to undo.
        }
        noisyRegistered = false;
    }

    // --- Talking to Go -----------------------------------------------------

    private void emitCommand(String command) {
        try {
            JSONObject o = new JSONObject();
            o.put("command", command);
            WailsBridge.emitFromService(COMMAND_EVENT, o.toString());
        } catch (Exception e) {
            Log.e(TAG, "command emit failed: " + command, e);
        }
    }

    private void emitDuck(boolean on) {
        try {
            JSONObject o = new JSONObject();
            o.put("command", "duck");
            o.put("on", on);
            WailsBridge.emitFromService(COMMAND_EVENT, o.toString());
        } catch (Exception e) {
            Log.e(TAG, "duck emit failed", e);
        }
    }

    @Override
    public void onDestroy() {
        running = false;
        unregisterNoisy();
        abandonFocus();
        if (session != null) {
            session.setActive(false);
            session.release();
            session = null;
        }
        artExecutor.shutdownNow();
        super.onDestroy();
    }

    @Nullable
    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}
