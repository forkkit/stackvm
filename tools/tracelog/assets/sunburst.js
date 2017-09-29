"use strict";

import "./sunburst.scss";

class EventEmitter {
    constructor() {
        this.events = new Map();
    }
    addListener(name, callback) {
        if (this.events.has(name)) {
            this.events.get(name).push(callback);
        } else {
            this.events.set(name, [callback]);
        }
    }
    removeListener(name, callback) {
        if (this.events.has(name)) {
            let callbacks = this.events.get(name).filter((cb) => cb !== callback);
            if (callbacks.length) {
                this.events.set(name, callbacks);
            } else {
                this.events.delete(name);
            }
        }
    }
    emit(name, ...args) {
        if (this.events.has(name)) {
            this.events.get(name).forEach((cb) => cb(...args));
        }
    }
}

let fmt = {};

fmt.id = (x) => x;

fmt.escapeHTML = (unsafe) => ("" + unsafe)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");

fmt.escaped = (f) => (...args) => fmt.escapeHTML(f(...args));

fmt.feid = fmt.escaped(fmt.id);

fmt.entries = (kfmts, filter) => {
    if (typeof filter === "function") {
        return (o) => Object.entries(o)
            .filter(([k]) => filter(k))
            .map(([k, v]) => [fmt.feid(k), (kfmts[k] || fmt.feid)(v)])
            .map(([k, v]) => `${k}=${v}`)
            .join(" ");
    }
    return (o) => Object.entries(o)
        .map(([k, v]) => [fmt.feid(k), (kfmts[k] || fmt.feid)(v)])
        .map(([k, v]) => `${k}=${v}`)
        .join(" ");
};

fmt.parseInt = (base) => (s) => parseInt(s, base);

fmt.hex = (n) => {
    let s = n.toString(16);
    s = "0" + s;
    if (s.length % 2 == 1) s = "0" + s;
    return s;
};

fmt.num = (base) => {
    base = base || 10;
    return (n) => n.toString(base);
};

fmt.all = (...fs) => (d) => fs.map((f) => f(d));

fmt.then = (...fs) => (s) => {
    for (var i = 0; i < fs.length; ++i) s = fs[i](s);
    return s;
};

fmt.dec2hex = fmt.then(fmt.parseInt(10), fmt.hex);

fmt.padded = (c, width, fmt) => {
    return (n) => {
        let s = fmt(n);
        if (s.length < width) s = c.repeat(width - s.length) + s;
        return s;
    };
};

fmt.replaceAll = (pat, f) => (s) => {
    let i = 0, j = 0, t = "";
    let pass = (thru) => {
        j = thru;
        t += s.slice(i, j);
        i = j;
    };
    let emit = (ss, skip) => {
        t += ss;
        i = j = j + skip;
    };
    if (pat.global) {
        for (let match = pat.exec(s); match; match = pat.exec(s)) {
            pass(match.index);
            emit(f(match[0]), match[0].length);
        }
    } else {
        let match = pat.exec(s);
        if (match) {
            pass(match.index);
            emit(f(match[0]), match[0].length);
        }
    }
    t += s.slice(j);
    return t;
};

import {
    hierarchy as d3Hierarchy,
    partition as d3Partition
} from "d3-hierarchy";
import {select as d3Select} from "d3-selection";
import {arc as d3Arc} from "d3-shape";

import { default as debounce } from "debounce";

const midPat = /^(\d+)\((\d+):(\d+)\)$/;
const numColors = 4;

class SunburstModel extends EventEmitter {
    constructor(sessions) {
        super();
        this.sessions = sessions;
        this.byID = new Map();
        this.kids = new Map();
        this.byOutcome = new Map();
        this.results = new Map();
        this.rootIDs = new Set();
        this.root = null;
        this._cur = null;

        this.sessions.forEach(d => {
            if (d.parent_id === null) {
                this.rootIDs.add(d.id);
            }
            let idm = midPat.exec(d.id);
            d.rootMID = parseInt(idm && idm[1]);
            d.parentMID = parseInt(idm && idm[2]);
            d.machID = parseInt(idm && idm[3]);
            this.byID.set(d.id, d);
            if (d.parent_id !== null) {
                if (this.kids.has(d.parent_id)) {
                    this.kids.get(d.parent_id).push(d.id);
                } else {
                    this.kids.set(d.parent_id, [d.id]);
                }
            }
            d.records = d.records.map((record) => {
                let {ip} = record;
                let loc = {ip, label: null};
                record.loc = loc;
                return record;
            });
        });

        let rootMID = 0, rootID = null;
        for (let s of this.sessions) {
            if (this.rootIDs.has(s.id)) {
                if (rootMID == 0 || s.machID < rootMID) {
                    rootMID = s.machID;
                    rootID = s.id;
                }
            }
        }
        this.rootID = rootID;
    }

    get rootID() {
        return this.root && this.root.data && this.root.data.id;
    }

    get rootMID() {
        return this.root && this.root.data && this.root.data.machID;
    }

    set rootID(rootID) {
        this.byOutcome = new Map();
        this.results = new Map();

        this.root = d3Hierarchy(this.byID.get(rootID), ({id}) => (
            this.kids.has(id)
                ? this.kids.get(id).map((cid) => this.byID.get(cid))
                : []))
            .sum(({records}) => records.filter(({kind}) => kind === "postOp").length)
            .sort(({data: {machID: a}}, {data: {machID: b}}) => a - b);

        for (let s of this.rootSessions) this.addOutcome(s);

        for (let resultID of this.byOutcome.get("values") || []) {
            let goal = this.byID.get(resultID);
            var last = goal;
            for (let node = goal; node; node = this.byID.get(node.parent_id)) {
                let res = {
                    nodeID: node.id,
                    nodeIDI: node.machID,
                    nodeCount: node.records[node.records.length-1].count,
                    resultID: goal.id,
                    resultIDI: goal.machID,
                };
                if (node !== goal) {
                    let lastCopy = node.records
                        .filter(({kind}) => kind === "copy")
                        .map(({count, extra}) => Object.assign({count}, extra))
                        .filter(({child}) => child === last.id)
                        .shift();
                    if (lastCopy) res.nodeCount = lastCopy.count;
                }
                this.results.set(node.machID, res);
                last = node;
            }
        }
    }

    get rootSessions() {
        let rootMID = this.rootMID;
        return this.sessions.filter((s) => s.rootMID == rootMID);
    }

    MID2ID(mid) {
        for (let s of this.sessions) {
            if (s.machID === mid) return s.id;
        }
        return null;
    }

    addOutcome(d) {
        let name = "unknown";
        if (d.error !== "") {
            name = `err=${d.error}`;
        } else if (d.values !== "") {
            name = "values";
        }
        if (this.byOutcome.has(name)) {
            this.byOutcome.get(name).push(d.id);
        } else {
            this.byOutcome.set(name, [d.id]);
        }
    }

    decorateRecordClass(record, name) {
        let parts = name.split(/\s+/);
        if (this.results.has(record.machID)) {
            let res = this.results.get(record.machID);
            if (record.count <= res.nodeCount) {
                parts.push(res.resultIDI === record.machID ? "goal" : "goalPath");
            }
        }
        return parts.join(" ");
    }

    decorateClass(machID, name) {
        let parts = name.split(/\s+/);
        if (this.results.has(machID)) {
            let res = this.results.get(machID);
            parts.push(res.resultIDI === machID ? "goal" : "goalPath");
        }
        return parts.join(" ");
    }

    findPath(id) {
        let idis = [];
        for (
            let node = this.byID.get(id);
            node;
            node = this.byID.get(node.parent_id)
        ) idis.unshift(node.machID);
        let g = this.root, path = [g];
        if (idis[0] !== g.data.machID) return [];
        idis.shift();
        descend: for (let i = 0; i < idis.length; i++) {
            for (const kid of g.children) {
                if (kid.data.machID === idis[i]) {
                    g = kid;
                    path.push(g);
                    continue descend;
                }
            }
            return null;
        }
        return path;
    }

    get cur() {
        return this._cur;
    }
    set cur(cur) {
        this._cur = cur;
        this.emit("curChanged", cur);
    }
}

function thel(elOrString) {
    if (typeof elOrString === "string") {
        return document.querySelector(elOrString);
    }
    return elOrString;
}

class SunburstChart extends EventEmitter {
    constructor(el) {
        super();
        this.el = thel(el);
        this.sel = d3Select(this.el);
        this.partition = d3Partition();
        this.cont = this.sel.append("g");
        this.bound = this.cont.append("g").attr("id", "bound");
        this.boundCirc = this.bound.append("circle");
        this.boundHead = this.bound.append("rect");
        this.arc = d3Arc()
            .startAngle(({x0}) => x0)
            .endAngle(({x1}) => x1)
            .innerRadius(({y0}) => Math.sqrt(y0))
            .outerRadius(({y1}) => Math.sqrt(y1));
        this.path = null;
        this._model = null;
        this.active = false;
        this.activate();
        this.bound.on("click", () => {
            let d = this._model.cur[this._model.cur.length - 1];
            this.emit("nodeActivated", d.data);
        });
    }

    set model(model) {
        this._model = model;
        this.size();
    }

    activate() {
        this.active = true;
        if (this.path) this.path.on("mouseover", (d) => this.mouseover(d));
    }

    deactivate() {
        this.active = false;
        if (this.path) this.path.on("mouseover", null);
    }

    blur() {
        this.sel.classed("focusing", false);
        this.sel.selectAll("path").classed("focus", false);
    }

    mouseover(d) {
        this._model.cur = d && d.ancestors().reverse();
        this.sel.classed("focusing", true);
        this.path.classed("focus", (node) => this._model.cur.indexOf(node) >= 0);
    }

    clicked(d) {
        this._model.cur = d && d.ancestors().reverse();
        this.emit("nodeActivated", d.data);
    }

    size() {
        const width = this.el.parentNode.clientWidth;
        const height = this.el.parentNode.clientHeight;
        const radius = Math.min(width, height) / 2;
        this.partition.size([2 * Math.PI, radius * radius]);
        this.sel
            .attr("width", width)
            .attr("height", height);
        this.cont.attr("transform", `translate(${width/2},${height/2})`);
        this.boundCirc.attr("r", radius);
        this.boundHead
            .attr("x", -width/2)
            .attr("y", -height/2)
            .attr("width", width)
            .attr("height", height*0.33);
        if (this._model !== null) this.draw();
    }

    draw() {
        this.path = this.cont
            .data([this._model.rootSessions])
            .selectAll("path")
            .data(this.partition(this._model.root).descendants());
        let enter = this.path
            .enter().append("path")
            .attr("fill-rule", "evenodd")
            .on("click", (d) => this.clicked(d));
        if (this.active) enter.on("mouseover", (d) => this.mouseover(d));
        this.path = this.path.merge(enter);
        this.path
            .attr("display", ({depth}) => depth ? null : "none")
            .attr("d", this.arc)
            .attr("class", ({depth, data: {machID}}) => this._model.decorateClass(
                machID, `fillColor${depth % numColors + 1}`));
    }
}

class SunburstTrail {
    constructor(el) {
        this.el = thel(el);
        this.sel = d3Select(this.el);
        this.items = null;
        this.activationCallback = null;
        this._model = null;
    }

    set model(model) {
        this._model = model;
        this._model.addListener("curChanged", (cur) => this.update(cur));
        this.update(this._model.cur);
    }

    update(cur) {
        cur = cur || [];
        let items = this.sel
            .selectAll("li")
            .data(cur, ({data, depth}) => data.id + depth);
        items.exit().remove();
        this.items = items.merge(items.enter()
            .append("li")
            .on("click", this.activationCallback));
        this.items
            .attr("class", ({depth, data: {machID}}) => this._model.decorateClass(
                machID, `bgColor${depth % numColors + 1}`))
            .text(({data: {machID}}) => machID);
    }

    activate(callback) {
        this.activationCallback = callback;
        this.el.className = "active";
        this.items.on("click", callback);
    }

    deactivate() {
        this.activationCallback = null;
        this.el.className = "";
        this.items.on("click", null);
    }
}

class RawRecordAssembler {
    constructor(model, nodeOrID) {
        this.model = model;
        this.node = null;
        this.nodes = [];
        if (typeof nodeOrID === "string") {
            this.node = this.model.byID.get(nodeOrID);
        } else {
            this.node = nodeOrID;
        }
        for (
            let node = this.node;
            node;
            node = this.model.byID.get(node.parent_id)
        ) this.nodes.unshift(node);
    }

    records(depth) {
        let {records} = this.nodes[depth];
        const next = this.nodes[depth+1];
        if (!next) return records;
        for (let i = 0; i < records.length; i++) {
            if (records[i].kind === "copy" &&
                records[i].extra["child"] === next.id) {
                return records.slice(0, i+1);
            }
        }
        return records;
    }
}

class RecordAssembler extends RawRecordAssembler {
    records(depth) {
        let {records} = this.nodes[depth];
        const next = this.nodes[depth+1];
        let out = [];
        let copy = null, preOp = null;
        for (let i = 0; i < records.length; i++) {
            let rec = records[i];
            switch (rec.kind) {
            case "begin":
                if (depth === 0) out.push(rec);
                break;

            case "preOp":
                preOp = rec;
                break;

            case "postOp":
                rec = Object.assign({preOp}, rec);
                if (preOp && preOp.extra.labels) {
                    rec.loc.label = preOp.extra.labels[0];
                }
                preOp = null;
                if (copy) {
                    rec.extra = Object.assign({}, copy.extra, rec.extra);
                    copy = null;
                }
                out.push(rec);
                break;

            case "copy":
                copy = rec;
                if (next && copy.extra["child"] === next.id) {
                    rec = records[++i];
                    if (rec.kind === "postOp") {
                        rec = Object.assign({}, rec, {
                            extra: Object.assign({}, copy.extra, rec.extra),
                        });
                        out.push(rec);
                    }
                    return out;
                }
                break;

            default:
                out.push(rec);
            }
        }
        return out;
    }
}

class LogTable {
    constructor(el) {
        this.ranges = new Map();
        this.el = thel(el);
        this._model = null;
        this.extraPluck = ["ps", "cs"];
        this.extraIgnore = new Set([
            "cbp", "csp",
            "pbp", "psp",
            "labels",
            "opName",
            "spanOpen", "spanClose",
        ].concat(this.extraPluck));
        this.head = this.el.tHead || this.el.appendChild(document.createElement("thead"));
        this.header = d3Select(this.head.appendChild(document.createElement("tr")));
        this.body = d3Select(this.el.tBodies[0] || this.el.appendChild(document.createElement("tbody")));
        this.raw = false;
        this.rawCols = [
            {title: "ID", className: "id"},
            {title: "#", className: "count"},
            {title: "IP", className: "ip"},
            {title: "Action", className: "action"},
            {title: "Extra", className: "extra"},
        ];
        this.cols = this.rawCols;
        this.baseFmt = [
            fmt.num(10),
            fmt.num(10),
            LogTable.locSpanFmt,
            LogTable.mungeActionFmt,
            LogTable.extraFmt((k) => !this.extraIgnore.has(k)),
        ];
        this.rawFmt = [
            fmt.num(10),
            fmt.num(10),
            LogTable.locIPFmt,
            fmt.feid,
            LogTable.extraFmt(),
        ];
        this.normFmt = this.baseFmt;
        this.ra = null;
    }

    set model(model) {
        this._model = model;

        //// setup basic columns
        this.cols = this.rawCols.slice(0, 4);
        this.normFmt = this.baseFmt.slice(0, 4);

        // discover max widths from data
        let idWidth = 0;
        let cntWidth = 0;
        this._model.rootSessions.forEach(({machID, records}) => {
            idWidth = Math.max(idWidth, this.normFmt[0](machID).length);
            records.forEach(({count}) => {
                cntWidth = Math.max(cntWidth, this.normFmt[1](count).length);
            });
        });
        this.normFmt[0] = fmt.padded(" ", idWidth, this.normFmt[0]);
        this.normFmt[1] = fmt.padded(" ", cntWidth, this.normFmt[1]);

        //// setup columns for plucked extra values
        this.cols = this.cols.concat(this.extraPluck
            .map((name) => ({title: name, className: name})));
        this.normFmt = this.normFmt.concat(this.extraPluck.map((k) => {
            switch (k) {
            case "ps":
                return (ns) => Array.isArray(ns) ? ns.join(" ") : ns;
            case "cs":
                return (ns) => Array.isArray(ns) ? ns.map(fmt.hex).join(" ") : ns;
            default:
                return fmt.feid;
            }
        }));

        //// setup final catch-all extra column
        this.cols.push(this.rawCols[4]);
        this.normFmt.push(this.baseFmt[4]);
    }

    focus(depth) {
        if (this.ranges.has(depth)) {
            let {start, end} = this.ranges.get(depth);
            let rows = this.el.tBodies[0].rows;
            for (var i = start; i < rows.length && i < end; ++i) {
                if (rows[i].style.display !== "none") {
                    rows[i].scrollIntoView();
                    return;
                }
            }
        }
    }

    show(node, raw) {
        this.raw = !!raw;
        this.ra = this.raw
            ? new RawRecordAssembler(this._model, node)
            : new RecordAssembler(this._model, node);
        this.update();
        this.head.scrollIntoView();
    }

    updateCells(each) {
        this.ranges.clear();

        let records = [];
        for (var depth = 0; depth < this.ra.nodes.length; ++depth) {
            let {machID} = this.ra.nodes[depth];
            let start = records.length;
            for (let r of this.ra.records(depth)) {
                let {count, loc, action, extra} = r;
                if (each) each(r, records.length);
                let cells = [machID, count, loc, action];
                if (!this.raw) for (let k of this.extraPluck) cells.push(extra[k] || "");
                extra = Object.assign({notes: r.notes}, extra);
                cells.push(extra);
                records.push({depth, machID, count, loc, cells});
            }
            this.ranges.set(depth, {start, end: records.length});
        }

        let colsel = this.header.selectAll("th").data(this.raw ? this.rawCols : this.cols);
        colsel.exit().remove();
        colsel = colsel.merge(colsel.enter().append("th"));
        colsel
            .attr("class", ({className}) => className)
            .text(({title}) => title);

        let rows = this.body.selectAll("tr").data(records);
        rows.exit().remove();
        rows = rows.merge(rows.enter().append("tr"));
        rows
            .style("display", "")
            .attr("class", (record) => this._model.decorateRecordClass(
                record, `bgColor${record.depth % numColors + 1}`));

        let cells = rows.selectAll("td").data(({cells}) => cells);
        cells.exit().remove();
        cells = cells.merge(cells.enter().append("td"));
        let cols = this.raw ? this.rawCols : this.cols;
        let fmt = this.raw ? this.rawFmt : this.normFmt;
        cells
            .attr("class", (_, i) => cols[i].className)
            .html((d, i) => fmt[i](d));

        return rows;
    }

    update() {
        if (this.raw) {
            this.updateCells();
            return;
        }

        let stack = [];

        let finish = (callLoc, reci) => {
            callLoc.span.end = reci;
            if (stack.length) {
                callLoc.span.parent = stack[stack.length-1];
                callLoc.span.parent.span.children.push(callLoc);
            }
        };

        let finishAny = (labels, reci) => {
            let labSet = new Set(labels);
            while (stack.length) {
                let callLoc = stack[stack.length-1];
                let match = false;
                for (let label of labSet) {
                    if (label === callLoc.label) {
                        labSet.delete(label);
                        match = true;
                        break;
                    }
                    let i = label.lastIndexOf(".");
                    if (i > 0 && label.slice(i+1) === callLoc.label) {
                        labSet.delete(label);
                        match = true;
                        break;
                    }
                }
                if (match) finish(stack.pop(), reci);
                else break;
            }
        };

        let rows = this.updateCells((rec, reci) => {
            let {loc, action, preOp} = rec;
            if (preOp && preOp.extra.spanClose) finishAny(preOp.extra.labels, reci);
            if (preOp && preOp.extra.spanOpen) {
                let open = new Set(preOp.extra.labels);
                let last = stack.length > 0 ? stack[stack.length-1] : null;
                if (!last || !last.label || !open.has(last.label)) {
                    loc.span = {
                        start: reci,
                        end: -1,
                        parent: null,
                        children: [],
                    };
                    stack.push(loc);
                }
            }
            if (action === "End") {
                while (stack.length) finish(stack.pop(), reci);
            }
        });

        let rowEls = rows.nodes();
        rows.select(".span").each(({loc: {span: {start, end}}}) => {
            if (end < start) return;
            for (let i = start+1; i <= end; i++) rowEls[i].style.display = "none";
        });
        rows.select(".span").on("click", ({loc: {span: {start, end, children}}}, i, x) => {
            const sel = d3Select(x[i]);
            const open = !sel.classed("open");
            sel.classed("open", open);
            if (end < start) return;
            if (!open) {
                for (let i = start+1; i <= end; i++) {
                    rowEls[i].style.display = "none";
                    let openSpan = rowEls[i].querySelector(".span.open");
                    if (openSpan) openSpan.className = openSpan.className
                        .split(/\s+/)
                        .filter((n) => n !== "open")
                        .join(" ");
                }
                return;
            }
            if (!children.length) {
                for (let i = start+1; i <= end; i++) {
                    rowEls[i].style.display = "";
                }
                return;
            }

            let j = 0;
            let childSpan = children[j].span;
            for (let i = start+1; i <= end; i++) {
                if (childSpan) {
                    if (i > childSpan.end) {
                        j++;
                        childSpan = j < children.length ? children[j].span : null;
                    } else if (i > childSpan.start) {
                        continue;
                    }
                }
                rowEls[i].style.display = "";
            }
        });
    }
}

LogTable.locIPFmt = ({ip}) => fmt.hex(ip);

LogTable.locLabelFmt = (rec) => {
    let addr = LogTable.locIPFmt(rec);
    if (rec.label) {
        if (rec.label[0] === ".") {
            addr = `<div title="${rec.label}:" class="label">${addr}</div>`;
        } else {
            addr = `<div title="@${addr}" class="label">${rec.label}:</div>`;
        }
    }
    return addr;
};

LogTable.locSpanFmt = (rec) => {
    let addr = LogTable.locLabelFmt(rec);
    if (rec.span) addr = `<div class="${rec.span.end < rec.span.start ? "brokenSpan" : "span"}">${addr}</div>`;
    return addr;
};

LogTable.mungeActionFmt = fmt.escaped((action) => action.replace(
    /([@+-])0x([0-9a-fA-F]+)/,
    (_m, sign, str) => sign + fmt.hex(parseInt(str, 16))));

LogTable.extraFmts = {
    child: (id) => `<a href="#${id}">${id}</a>`,
    parent: (id) => `<a href="#${id}">${id}</a>`,
};

LogTable.notesFmt = (notes) => notes &&
    `<details><summary>notes=</summary>${notes.join("\n")}</details>`;

LogTable.extraFmt = (filter) => fmt.then(fmt.all(
    fmt.entries(LogTable.extraFmts, filter
        ? (k) => k !== "notes" && filter(k)
        : (k) => k !== "notes"),
    ({notes}) => LogTable.notesFmt(notes)
), (parts) => parts.filter((part) => part).join(" "));

class Links {
    constructor(el) {
        this.el = thel(el);
        this.sel = d3Select(this.el);
        this._model = null;
        this.groups = null;
        this.links = null;
    }

    set model(model) {
        this._model = model;
        this.update();
    }

    update() {
        let dat = [];
        for (const [name, ids] of this._model.byOutcome) {
            dat.push({name, ids});
        }
        dat.sort((a, b) => a.name < b.name ? -1 : a.name > b.name ? 1 : 0);

        this.groups = this.sel.selectAll("details").data(dat);
        let enter = this.groups.enter().append("details");
        enter.append("summary");
        enter.append("ul");
        this.groups = this.groups.merge(enter);
        this.groups.select("summary")
            .text(({name}) => name);

        this.links = this.groups.select("ul")
            .selectAll("li")
            .data(({ids}) => ids);
        enter = this.links.enter().append("li").append("a");
        this.links = this.links.select("a").merge(enter);
        this.links
            .attr("href", (machID) => `#${machID}`)
            .text((machID) => machID);
    }
}

class Page {
    constructor(chartEl, trailEl, logEl, linksEl, statsEl, rootsEl) {
        this.rootsEl = thel(rootsEl);
        this.chart = new SunburstChart(chartEl);
        this.trail = new SunburstTrail(trailEl);
        this.log = new LogTable(logEl);
        this.links = new Links(thel(linksEl));
        this.stats = d3Select(thel(statsEl));
        this.roots = d3Select(this.rootsEl);
        this.model = null;
        this.handleLogKeyUp = (e) => { if (e.keyCode == 27) this.showChart(); };
        this.chart.addListener("nodeActivated", (node) => this.showLog(node));
        let mouseleave = debounce(() => {
            if (this.chart.active) {
                this.model.cur = null;
                this.chart.blur();
            }
        }, 200);
        this.trail.el.addEventListener("mouseover", mouseleave.clear);
        this.chart.cont.on("mouseleave", mouseleave);
        this.rootsEl.addEventListener("change", (e) => this.onRootChanged(e));
    }

    onRootChanged(e) {
        let s = this.model.byID.get(e.target.value);
        if (s) window.location.hash = `#${s.machID}`;
    }

    showChart() {
        window.location.hash = "";
        this.chart.el.style.display = "";
        this.log.el.style.display = "none";
        this.trail.deactivate();
        this.chart.activate();
        window.removeEventListener("keyup", this.handleLogKeyUp);
    }

    showLog(node, tags) {
        tags = (tags || []).filter((tag) => tag === "raw");
        let raw = tags.length === 1;
        let parts = ["", node.id].concat(tags);
        let canonical = parts.join("#");
        if (window.location.hash !== canonical) {
            window.location.hash = canonical;
            return;
        }
        this.chart.el.style.display = "none";
        this.log.el.style.display = "";
        this.chart.deactivate();
        this.trail.activate((_, i) => this.log.focus(i));
        window.addEventListener("keyup", this.handleLogKeyUp);
        this.log.show(node, raw);
    }

    nav() {
        if (!this.model) return;
        let parts = window.location.hash.split(/#/).slice(1);
        if (!parts.length) {
            window.location.hash = `#${this.model.byID.get(this.model.rootID).machID}`;
            return;
        }

        let id = parts[0];

        if (this.model.byID.has(id)) {
            let idm = midPat.exec(id);
            this.model.rootID = this.model.MID2ID(parseInt(idm[1]));
            this.rootsEl.value = this.model.rootID;

            let path = this.model.findPath(id);
            if (path !== null) {
                this.model.cur = path;
                this.showLog(path[path.length-1].data, parts.slice(1));
                this.size();
                return;
            }
        }

        let idm = /^\d+$/.exec(id);
        if (idm) {
            let mid = parseInt(idm[0]);
            let id = this.model.MID2ID(mid);
            let node = this.model.byID.get(id);
            if (node) {
                if (mid !== node.rootMID) {
                    window.location.hash = `#${node.rootMID}`;
                    return;
                }
                this.model.rootID = node.machID === node.rootMID ? node.id : this.model.MID2ID(node.rootMID);
                this.rootsEl.value = this.model.rootID;
                this.size();
                return;
            }
        }

        window.location.hash = "";
    }

    load(data) {
        (data instanceof Promise ? data : Promise.resolve(data)
        ).then((data) => {
            this.model = new SunburstModel(data);
            this.trail.model = this.model;
            this.chart.model = this.model;
            this.log.model = this.model;
            this.links.model = this.model;

            this.roots.style("display", this.model.rootIDs.size > 1 ? "" : "none");
            let rootSel = this.roots
                .selectAll("option")
                .data(Array.from(this.model.rootIDs).sort());
            rootSel.exit().remove();
            rootSel = rootSel.merge(rootSel.enter().append("option"));
            rootSel.attr("value", (d) => d);
            rootSel.text((d) => d);

            this.nav();
        });
    }

    size() {
        this.chart.size();
        this.links.update();
        this.draw();
    }

    draw() {
        let stats = {
            Machines: this.model.rootSessions.length,
            Operations: this.model.root.value,
        };

        let sel = this.stats.selectAll("tr").data(Object.entries(stats));
        sel.exit().remove();
        sel = sel.merge(sel.enter().append("tr"));
        sel = sel.selectAll("td").data(([k, v]) => [v, k]);
        sel.exit().remove();
        sel = sel.merge(sel.enter().append("td")).text((d) => d);
    }
}

let pg = new Page("#chart", "#sequence", "#log", "#links", "#stats", "#root");
window.addEventListener("resize", () => pg.size());
window.addEventListener("hashchange", () => pg.nav());
pg.load(window[document.querySelector("script.main").dataset.var]);
