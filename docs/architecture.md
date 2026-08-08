# SentinelDesk architecture

One shared Linux desktop, two independent control planes.

This diagram is the source, not an export of one. It replaced a PNG whose source
was not in this repository, and which by the time anybody checked said the
catalogue held 106 tools and — twice, in the first image of the README — that
room arbitration did not apply to MCP. That second one was not stale, it was
backwards: every tool that drives the desktop passes through the same gate
whichever plane asked for it. A picture nobody can diff says whatever it said on
the day it was drawn, forever.

```mermaid
flowchart TB
    subgraph host["AI host — outside the container"]
        agent["Claude Code / Claude Desktop / any MCP host<br/>runs: docker exec … sentineldesk -mcp-stdio<br/><i>killing the host does not stop the desktop</i>"]
    end

    subgraph browsers["Browsers — up to MAX_VIEWERS, default 4"]
        b1["&lt;video&gt; + &lt;audio&gt;<br/>DataChannel 'input'<br/>outgoing microphone track"]
    end

    turn["coturn / TURN relay<br/><i>needed on macOS and Windows<br/>with Docker Desktop</i>"]

    subgraph container["Container — Debian 13, supervisord"]
        subgraph desktop["Shared Linux desktop"]
        direction LR
            xvfb["Xvfb :0<br/><i>virtual display, no monitor</i>"]
            ob["Openbox<br/><i>window manager</i>"]
            fm["pcmanfm --desktop<br/><i>owns the root window</i>"]
            panel["lxpanel<br/><i>top panel</i>"]
            apps["Applications<br/>Chromium, lxterminal + tmux,<br/>VLC, TigerVNC, FreeRDP, Steam"]
            xvfb --> ob --> fm --> panel --> apps
        end

        subgraph pipes["Media"]
            vout["ximagesrc use-damage<br/>go-gst, in-process<br/><i>nvenc → vaapi → x264 → vp8</i><br/>live bitrate adjustment"]
            aout["PulseAudio null sink<br/>opusenc"]
            ain["upstream mic<br/>module-remap-source"]
        end

        subgraph daemon["sentineldesk — one Go binary"]
            room["Room<br/><i>one capture, fanned out<br/>exactly one controller<br/>bitrate = min estimate</i>"]
            rec["Recorder<br/><i>gst-launch children</i>"]
            deliv["Delivery"]
            pp["PeerPointers<br/><i>real X windows</i>"]
        end

        subgraph doors["The two doors"]
            ws["WS /ws<br/><b>the only door for humans</b><br/>first frame must be auth<br/>SDP · ICE · presence after"]
            http["HTTP :8080<br/>web UI, files, tickets<br/><b>holds no secrets</b><br/>/auth only says IF a login is needed"]
            sock["Unix socket 0600<br/>/run/user/1000/sentineldesk-mcp.sock<br/>MCP server — 121 tools"]
        end

        subgraph bridges["Helper bridges"]
            a11y["a11y.py<br/>AT-SPI → ui_* tools"]
            cdp["Chromium CDP :9222<br/>browser_* tools"]
        end

        gate{{"mayInject<br/><b>the control gate</b>"}}
        inject["XTEST · uinput<br/><i>keyboard, mouse, gamepad</i>"]

        vout --> room
        aout --> room
        room --> deliv
        ain --> room
        sock --> gate
        ws --> gate
        gate --> inject
        inject --> xvfb
        sock --> bridges
        cdp --> apps
        a11y --> apps
    end

    b1 <-->|"DTLS / SRTP / ICE"| deliv
    b1 <-->|"signalling · presence · input"| ws
    b1 -.->|"PLI → keyframe<br/>GCC / TWCC → bitrate"| room
    agent -->|"JSON-RPC over stdin/stdout"| sock
    turn -.-> b1

```

## The part the old picture got wrong

**Control is claimed, never assumed — by either plane.** Every tool listed in
`injectsInput()` and every tool that changes the shared screen passes through
`mayInject()` before it reaches the desktop, so the agent must hold the controls
first. It never takes them implicitly, not even with the room empty. Releasing
sets the controller to `ControlFree` rather than handing it on, and joining does
not grant it.

Room control and the MCP policy answer different questions. The first is who is
driving right now; the second is what the agent may ever do. To stop an agent
touching anything, the instrument is `-mcp-policy readonly`.

## The two planes

| | humans | the agent |
|---|---|---|
| door | `WS /ws` | Unix socket, mode `0600` |
| first thing | `{type:"auth"}`, and nothing else works until it validates | JSON-RPC `initialize` |
| carries | SDP, ICE, TURN credentials, presence | tool calls |
| input arrives on | a DataChannel named `input` | tool calls that pass the gate |
| survives the other dying | yes | yes — `-mcp-stdio` is a separate process |

HTTP holds no secrets on purpose: ICE configuration and TURN credentials travel
over the authenticated WebSocket, and `/` is served openly because it is an
empty frame that can do nothing until it authenticates.

## What runs in-process and what does not

The **live** pipeline runs inside the Go process — Pion plus go-gst, no
`gst-launch` child. The side pipelines are deliberately children: recording,
screenshots, and the roomless `start_restream` fallback are built from
parameters an agent chooses, they are the ones that can fail on a bad codec
combination or a full disk, and in-process a fault in one of them would take the
daemon down and drop every viewer of a stream that had nothing to do with it.

Same reasoning as `-mcp-stdio` being a separate process: the desktop outlives
whatever went wrong beside it.
