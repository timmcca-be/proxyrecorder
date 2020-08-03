//@flow

/*::
type RecordInfo = {|
    requestID: number,
    operationType: "query" | "mutation",
    operationName: string,
    willSnapshot: boolean,
    snapshotComplete: boolean,
|}

type Record = {|
    requestID: number,
    operationType: string,
    operationName: string,
    request: string,
    response: string,
    currentSnapshot: string,
    priorSnapshot: string,
    notes: string,
|}

type InitMessage = {|
    type: "init",
    data: Array<RecordInfo>,
|}

type RecordMessage = {|
    type: "record",
    data: RecordInfo,
|}

type Message = InitMessage | RecordMessage;
*/

class Item {
    /*:: _recordInfo: RecordInfo */
    /*:: _selected: boolean */
    /*:: _element: HTMLDivElement */


    constructor(recordInfo /*: RecordInfo */, clickCallback /*: RecordInfo => void */) {
        this._selected = false
        this._recordInfo = recordInfo
        this._element = document.createElement("div");
        this._element.addEventListener("click", () => {
            clickCallback(this._recordInfo);
            this.setSelected(true)
        });
        this._update();
    }

    updateInfo(recordInfo /*: RecordInfo */) {
        this._recordInfo = recordInfo
        this._update();
    }

    setSelected(selected /*: boolean */) {
        this._selected = selected
        this._update()
    }

    element() {
        return this._element;
    }

    id() {
        return this._recordInfo.requestID;
    }

    _class() {
        return "c-request-list--item" + (this._selected ? " x--selected" : "");
    }

    _innerHTML() {
        const info = this._recordInfo
        let statusClass = "x--no-status";
        if (info.willSnapshot && info.snapshotComplete) {
            statusClass = "x--status-complete"
        }
        if (info.willSnapshot && !info.snapshotComplete) {
            statusClass = "x--status-pending"
        }
        let operationTypeName = "Q";
        if (info.operationType === "mutation") {
            operationTypeName = "M";
        }
        return `
            <div class="c-request-list--item--type x--${info.operationType}">
                ${operationTypeName}
            </div>
            <div class="c-request-list--item--name">
                ${this._recordInfo.operationName}
            </div>
            <div class="c-request-list--item--status ${statusClass}">
            </div>
        `;
    }

    _update() {
        this._element.className = this._class();
        this._element.innerHTML = this._innerHTML();
    }
}

class Content {
    /*:: _record: ?Record */
    /*:: _element: HTMLDivElement */

    constructor(record /*: ?Record */) {
        this._record = record
        this._element = document.createElement("div");
        this._element.className = "c-content-wrapper";
        this._update();
    }

    element() {
        return this._element;
    }

    updateRecord(record /*: ?Record */) {
        this._record = record;
        this._update();
    }

    _innerHTML() {
        if (this._record == null) {
            return `
                <div class="c-placeholder">
                    <div class="c-placeholder--message">
                        Click a request on the left for details.
                    </div>
                </div>
            `
        }

        const record /*: Record */ = this._record;

        let snapshotHeader = "Snapshot diff"
        let snapshot = `<div id="snapshot-diff-interface"></div>`;
        let buttons = `
            <div class="c-diff-buttons">
                <button class="js-prev-difference">Previous Difference</button>
                <button class="js-next-difference">Next Difference</button>
            </div>
        `;

        if (!record.priorSnapshot.length) {
            snapshotHeader = "Most recent snapshot"
            snapshot = `
                <pre class="c-verbatim-output x--limit-height">
${record.currentSnapshot}
                </pre>
            `
            buttons = "";
        }

        return `
            <div class="c-content-container">
                <h3>${snapshotHeader}</h3>
                ${buttons}
                ${snapshot}
                <h3>Request &bull; ${record.requestID}</h3>
                <pre class="c-verbatim-output">
${formatJSON(record.request)}
                </pre>
                <h3>Response</h3>
                <pre class="c-verbatim-output">
${formatJSON(record.response)}
                </pre>
            </div>
        `;
    }

    _update() {
        this._element.innerHTML = this._innerHTML();

        if (this._record == null) {
            return;
        }

        const record /*: Record */ = this._record;

        var target = this._element.querySelector("#snapshot-diff-interface");

        if (target == null) {
            return;
        }

        const dv = window.CodeMirror.MergeView(target, {
            value: record.priorSnapshot,
            orig: record.currentSnapshot,
            lineNumbers: true,
            mode: "application/json",
            connect: "align",
            collapseIdentical: true,
        });

        window.dv = dv;

        const next = this._element.querySelector(".js-next-difference");

        if (next) {
            next.addEventListener("click", () => {
                dv.editor().execCommand("goNextDiff");
            });
        }

        const prev = this._element.querySelector(".js-prev-difference");

        if (prev) {
            prev.addEventListener("click", () => {
                dv.editor().execCommand("goPrevDiff");
            })
        }
    }
}

function formatJSON(s /*: string */) {
    return JSON.stringify(JSON.parse(s), null, 4);
}

window.addEventListener('DOMContentLoaded', (event) => {
    const list = document.getElementById("request-list");

    if (list == null) {
        console.log("#request-list element not found");
        return;
    }

    const items = [];

    function clearAllSelections() {
        items.forEach(item => item.setSelected(false));
    }

    function handleClick(recordInfo /*: RecordInfo */) {
        clearAllSelections();
        loadRecord(recordInfo.requestID);
    }

    function addItem(recordInfo /*: RecordInfo */) {
        const item = new Item(recordInfo, handleClick);
        list.prepend(item.element());
        items.push(item);
    }

    function handleRecord(recordInfo /*: RecordInfo*/) {
        let found = false;
        
        items.forEach(item => {
            if (item.id() == recordInfo.requestID) {
                found = true;
                item.updateInfo(recordInfo);
            }
        });

        if (!found) {
            addItem(recordInfo);
        }
    }

    const socket = new WebSocket(`ws://${window.location.host}/ws`);

    // Listen for messages
    socket.addEventListener('message', function (event /*: MessageEvent */) {
        const data /*: any */ = event.data;
        const message /*: Message */ = JSON.parse(data);

        if (message.type === "init") {
            message.data.forEach(record => addItem(record));
        } else if (message.type === "record") {
            handleRecord(message.data);
        } else {
            console.error("unknown message type", message.type);
        }
    });

    // Content

    const contentContainer = document.getElementById("content");

    const content = new Content(null);

    if (contentContainer != null) {
        contentContainer.appendChild(content.element())
    } else {
        console.error("no content container");
    }

    function loadRecord(requestID /*: number */) {
        fetch(`/request?id=${requestID}`)
            .then(response => response.json())
            .then((record /*: any */) => content.updateRecord(record))
            .catch(error => console.error(error))
    }
});
