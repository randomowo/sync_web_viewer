let conn = null;

const MsgType = Object.freeze({
    USER_INIT: 1, SELECTOR_INIT: 2, VIDEO_INIT: 3, VIDEO: 4,
});

const VideoType = Object.freeze({
    Paused: 1, Playing: 2, Seek: 3,
});

const msgDelim = String.fromCharCode(0x00);
const payloadDelim = String.fromCharCode(0x01);

let vidEl = null;
let sourceList = null;
let videoReady = false;

const alert = (str) => {
    window.alert(str);
    if (conn !== null) {
        conn.close();
    }
};

window.onload = () => {
    initConn();

    sourceList = document.getElementById("vids");
    vidEl = document.getElementById("video-src");
    initVidEl();
};

function initConn() {
    conn = new WebSocket("/ws");

    conn.onclose = (e) => {
        console.log("socket closed:", e);
        setTimeout(initConn, 1000);
    };

    conn.onopen = (e) => {
        console.log("socket connected:", e);

        conn.send(createMessage(MsgType.USER_INIT, ""));
    };

    conn.onmessage = processMessage;

    conn.onerror = (e) => {
        console.log("error in socket:", e);
    };
}

function initVidEl() {
    vidEl.onpause = (e) => {
        conn.send(createMessage(MsgType.VIDEO, createVideoPayload(VideoType.Paused, vidEl.currentTime, sourceList.options[sourceList.selectedIndex].value)));
    };
    vidEl.onseeking = async (e) => {
        // await vidEl.pause();
    };
    vidEl.onstalled = vidEl.onpause;

    vidEl.onplay = (e) => {
        conn.send(createMessage(MsgType.VIDEO, createVideoPayload(VideoType.Playing, vidEl.currentTime, sourceList.options[sourceList.selectedIndex].value)));
    };
    vidEl.onseeked = (e) => {
        conn.send(createMessage(MsgType.VIDEO, createVideoPayload(VideoType.Seek, vidEl.currentTime, sourceList.options[sourceList.selectedIndex].value)));
    };
}

function createMessage(type, payload) {
    let userID = localStorage.getItem("userId");
    if (userID === null) {
        userID = "null";
    }

    return new Blob([type, msgDelim, userID, msgDelim, payload, msgDelim]);
}

function createVideoPayload(vt, seek, name) {
    return [vt, seek, name].join(payloadDelim);
}

async function processMessage(e) {
    let text = await e.data.text();
    console.log("new message", text);

    let data = text.split(msgDelim);
    if (data.length !== 3) {
        alert("wrong message size received");
        return;
    }

    let fn = null;

    switch (Number(data[0])) {
        case MsgType.USER_INIT:
            localStorage.setItem("userId", data[2]);
            conn.send(createMessage(MsgType.SELECTOR_INIT, ""));
            return;
        case MsgType.SELECTOR_INIT:
            fn = processSelectorInit;
            break;
        case MsgType.VIDEO_INIT:
            fn = processVideoInit;
            break;
        case MsgType.VIDEO:
            fn = processVideo;
            break;
        default:
            alert(`wrong message type ${data[0]}`)
            return;
    }

    if (fn === null) {
        alert(`unmapped func for msg type ${data[0]}`);
        return;
    }

    try {
        await fn(data[2]);
    } catch (e) {
        alert(`got error: ${e}`);
    }
}

async function processVideoInit(name) {
    // conn.send(createMessage(MsgType.VIDEO, createMessage(VideoType.Paused, 0, name)));
    //
    // while (sourceList === null) {
    //     conn.send(createMessage(MsgType.VIDEO, createMessage(VideoType.Paused, 0, name)));
    // }

    for (const optIndex in sourceList) {
        if (sourceList.options[optIndex].value === name) {
            if (!sourceList.options[optIndex].selected) {
                sourceList.options[optIndex].selected = true;
                await setVideo(name);
            }
            return;
        }
    }

    sourceList.options = [];
    sourceList = null;

    conn.send(createMessage(MsgType.SELECTOR_INIT, ""));

    // processVideoInit(name);
}

async function processVideo(payload) {
    let vidPayload = payload.split(payloadDelim);
    if (vidPayload.length !== 3) {
        alert("wrong payload");
        return;
    }

    if (vidEl === null || vidEl.children.length === 0) {
        return;
    }

    let time = Number(vidPayload[1]);
    if (isNaN(time)) {
        alert(`wrong seek value ${vidPayload[1]}`);
        return;
    }

    switch (Number(vidPayload[0])) {
        case VideoType.Paused:
            await vidEl.pause();
            vidEl.currentTime = time;
            break;
        case VideoType.Seek:
            vidEl.currentTime = time;
            break;
        case VideoType.Playing:
            vidEl.currentTime = time;
            await vidEl.play();
            break;
        default:
            alert(`wrong payload value ${vidPayload[0]}`);
            break;
    }
}

async function processSelectorInit(payload) {
    sourceList.options = [];

    sourceList.onchange = onOptionSelected;

    let vids = payload.split(payloadDelim);

    for (let index in vids) {
        let opt = document.createElement("option");
        opt.value = vids[index];
        opt.text = vids[index];

        sourceList.append(opt);
    }
}

async function onOptionSelected(e) {
    conn.send(createMessage(MsgType.VIDEO_INIT, e.target.value));

    await setVideo(e.target.value);
}

async function setVideo(name) {
    console.log("set video", name);
    vidEl.innerText = "";

    if (name === "null") {
        vidEl.hidden = true;
        return;
    }

    let source = document.createElement("source");
    source.src = `/content/${name}`;

    vidEl.hidden = false;
    vidEl.append(source);

    await vidEl.load();
}
