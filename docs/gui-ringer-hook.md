# GUI ringer hook

The intercom's display follows a ringer mute it sees on the bus but ignores an
unmute. The display keeps showing the ringer muted after an unmute from Home
Assistant or HomeKit. Without help, the companion reloads the firmware so the
display reads the state again (`internal/ringersync`), and that stops the
intercom for about 25 s.

The GUI hook avoids that reload. It is a small QML item added to the display's
GUI. The item asks the companion for the ringer state and sets the display's
own state with the GUI's `AudioState`.

## Why the display ignores an unmute

This was found by disassembling the Classe 100X firmware 1.5.8, read only. The
ringer status frame is `*#8**33*V##`: `V` is `0` for muted and `1` for
unmuted. Two places drop the unmute:

- **The vendor library** `libbtobjects.so`, in
  `AnsweringMachineDevice::parseFrame`, passes dimension 33 on to the GUI only
  when `V` is `0`.
- **The GUI** (`BtClass100`) handles that value only when it is true, by
  calling `enableState(RingtoneMute)`. It never calls `disableState`.

The display starts with the ringer on, which is why a reload fixes it. It does
not read the ringer state at start.

## How it works

The QML item long-polls `GET http://127.0.0.1:8080/api/v3/gui/ringer?muted=1|0`.

- **The request:** `muted` is what the display shows now.
- **The answer:** `{"muted": bool}` is what the display should show. It comes
  at once when the two differ. Otherwise it comes when the ringer changes, or
  after 25 s.
- **Loopback only:** the route answers only requests from loopback, and only
  `GET`. From the network it answers `404`.
- **Before the state is known:** until the companion has read the ringer from
  the bus, it answers what the display shows. Its default state could
  otherwise unmute a muted ringer.
- **A display that does not follow:** the same correction is sent at most once
  every 5 s, so such a display cannot ask in a tight loop.
- **No reload:** when a display has said, after an unmute, that it shows the
  ringer unmuted, `ringersync` skips the firmware reload. It logs `the display
  shows the ringer unmuted; no firmware reload`.
- **Fallback:** without the hook, or when the hook did not follow, the reload
  runs as before.

## The QML

The item goes inside the root `Item` of the GUI's `EventManager.qml`
(`/home/bticino/bin/gui/skins/default/`). That file imports `BtClass100 1.0`,
which provides `AudioState`, and is created once by `main.qml`. The other GUI
files stay as they are.

```qml
    QtObject {
        id: companionRinger

        property var request: null

        function next() {
            var xhr = new XMLHttpRequest()
            request = xhr
            xhr.onreadystatechange = function() {
                if (xhr.readyState !== XMLHttpRequest.DONE || request !== xhr)
                    return
                request = null
                var muted = null
                if (xhr.status === 200) {
                    try {
                        var body = JSON.parse(xhr.responseText)
                        if (typeof body.muted === "boolean")
                            muted = body.muted
                    } catch (error) {}
                }
                if (muted === null) {
                    companionRingerRetry.start()
                    return
                }
                if (muted !== global.audioState.isRingtoneMute) {
                    if (muted)
                        global.audioState.enableState(AudioState.RingtoneMute)
                    else
                        global.audioState.disableState(AudioState.RingtoneMute)
                }
                next()
            }
            xhr.open("GET", "http://127.0.0.1:8080/api/v3/gui/ringer?muted=" +
                (global.audioState.isRingtoneMute ? "1" : "0"))
            xhr.send()
        }
    }

    Timer {
        id: companionRingerRetry

        interval: 30000
        onTriggered: companionRinger.next()
    }

    Component.onCompleted: companionRinger.next()
```

## Installing it

The root filesystem is mounted read-only, and a firmware update puts the stock
file back.

1. Keep a copy of the stock file in `/home/bticino/cfg/extra`.
2. Run `mount -o remount,rw /`.
3. Copy the edited file over the stock one. Keep the owner `bticino:bticino`
   and mode `664`.
4. Run `sync`, then `mount -o remount,ro /`.
5. Reboot the intercom. On a C100X, killing the GUI alone rebooted the whole
   unit.

To undo it, copy the stock file back the same way.
