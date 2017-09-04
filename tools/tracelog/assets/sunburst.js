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
        this.rootID = null;
        this.root = null;
        this._cur = null;

        this.sessions.forEach(d => {
            if (d.parent_id === null) {
                if (this.rootID !== null) {
                    throw new Error("only one root supported");
                }
                this.rootID = d.id;
            }
            let idm = midPat.exec(d.id);
            d.idi = parseInt(idm && idm[3]);
            this.byID.set(d.id, d);
            if (d.parent_id !== null) {
                if (this.kids.has(d.parent_id)) {
                    this.kids.get(d.parent_id).push(d.id);
                } else {
                    this.kids.set(d.parent_id, [d.id]);
                }
            }
            this.addOutcome(d);
        });

        (this.byOutcome.get("values") || []).forEach((resultID) => {
            let res = this.byID.get(resultID);
            for (let node = res; node; node = this.byID.get(node.parent_id)) {
                this.results.set(node.idi, {
                    nodeID: node.id,
                    nodeIDI: node.idi,
                    resultID: res.id,
                    resultIDI: res.idi,
                });
            }
        });

        this.root = d3Hierarchy(this.byID.get(this.rootID), ({id}) => (
            this.kids.has(id)
                ? this.kids.get(id).map((cid) => this.byID.get(cid))
                : []))
            .sum(() => 1)
            .sort(({data: {idi: a}}, {data: {idi: b}}) => a - b);
        // .sort(({value: a}, {value: b}) => a - b);
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

    decorateClass(idi, name) {
        let parts = name.split(/\s+/);
        if (this.results.has(idi)) {
            let res = this.results.get(idi);
            parts.push(res.resultIDI === idi ? "goal" : "goalPath");
        }
        return parts.join(" ");
    }

    findPath(id) {
        let idis = [];
        for (
            let node = this.byID.get(id);
            node;
            node = this.byID.get(node.parent_id)
        ) idis.unshift(node.idi);
        let g = this.root, path = [g];
        if (idis[0] !== g.data.idi) return [];
        idis.shift();
        descend: for (let i = 0; i < idis.length; i++) {
            for (const kid of g.children) {
                if (kid.data.idi === idis[i]) {
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
            .data([this._model.sessions])
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
            .attr("class", ({depth, data: {idi}}) => this._model.decorateClass(
                idi, `fillColor${depth % numColors + 1}`));
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
            .attr("class", ({depth, data: {idi}}) => this._model.decorateClass(
                idi, `bgColor${depth % numColors + 1}`))
            .text(({data: {idi}}) => idi);
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
        let copy = null;
        for (let i = 0; i < records.length; i++) {
            let rec = records[i];
            switch (rec.kind) {
            case "begin":
                if (depth === 0) out.push(rec);
                break;

            case "preOp":
                break;

            case "postOp":
                if (copy) {
                    rec = Object.assign({}, rec, {
                        extra: Object.assign({}, copy.extra, rec.extra),
                    });
                    copy = null;
                }
                out.push(rec);
                break;

            case "copy":
                if (next && rec.extra["child"] === next.id) {
                    return out;
                }
                if (i !== 0) copy = rec;
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
        this.el = thel(el);
        this.sel = d3Select(this.el);
        this._model = null;
        this.extraPluck = ["ps", "cs"];
        this.extraIgnore = new Set([
            "parent", "child",
            "cbp", "csp",
            "pbp", "psp",
        ].concat(this.extraPluck));
        this.head = this.el.tHead || this.el.appendChild(document.createElement("thead"));
        this.header = d3Select(this.head.appendChild(document.createElement("tr")));
        this.raw = false;
        this.fmt = null;
        this.ra = null;
    }

    set model(model) {
        this._model = model;

        //// setup basic columns
        let cols = ["ID", "#", "IP", "Action"];
        this.fmt = LogTable.baseFmt.concat([LogTable.mungeActionFmt]);

        // discover max widths from data
        let idWidth = 0;
        let cntWidth = 0;
        let ipWidth = 0;
        this._model.sessions.forEach(({idi, records}) => {
            idWidth = Math.max(idWidth, this.fmt[0](idi).length);
            records.forEach(({count, ip}) => {
                cntWidth = Math.max(cntWidth, this.fmt[1](count).length);
                ipWidth = Math.max(ipWidth, this.fmt[2](ip).length);
            });
        });
        this.fmt[0] = fmt.padded(" ", idWidth, this.fmt[0]);
        this.fmt[1] = fmt.padded(" ", cntWidth, this.fmt[1]);
        this.fmt[2] = fmt.padded("0", ipWidth, this.fmt[2]);

        //// setup columns for plucked extra values
        cols = cols.concat(this.extraPluck);
        this.fmt = this.fmt.concat(this.extraPluck.map((k) => {
            switch (k) {
            case "cs":
                return fmt.replaceAll(/\d+/g, fmt.dec2hex);
            default:
                return fmt.id;
            }
        }));

        //// setup final catch-all extra column
        cols.push("Extra");
        this.fmt.push(fmt.id);

        //// update header
        let colsel = this.header.selectAll("th").data(cols);
        colsel.exit().remove();
        colsel = colsel.merge(colsel.enter().append("th"));
        colsel.text((col) => col);
    }

    focus(i) {
        this.el.tBodies[i].scrollIntoView();
    }

    show(node, raw) {
        this.raw = !!raw;
        this.ra = this.raw
            ? new RawRecordAssembler(this._model, node)
            : new RecordAssembler(this._model, node);
        this.update();
        this.head.scrollIntoView();
    }

    update() {
        let bodies = this.sel.selectAll("tbody").data(this.ra.nodes);
        bodies.exit().remove();
        bodies = bodies.merge(bodies.enter().append("tbody"));

        let rows = bodies.selectAll("tr")
            .data(({idi}, depth) => {
                let records = this.ra.records(depth);
                return records.map(r => Object.assign({depth, idi}, r));
            });
        rows.exit().remove();
        rows = rows.merge(rows.enter().append("tr"));
        rows.attr("class", ({depth, idi}) => this._model.decorateClass(
            idi, `bgColor${depth % numColors + 1}`));

        let cells = rows.selectAll("td")
            .data(({idi, count, ip, action, extra}) => {
                let r = [idi, count, ip, action];
                r = r.concat(this.extraPluck.map((k) => extra[k] || ""));
                r.push(Object.entries(extra)
                    .filter(([k]) => !this.extraIgnore.has(k))
                    .map(([k, v]) => `${k}=${v}`).join(" "));
                return r;
            });
        cells.exit().remove();
        cells = cells.merge(cells.enter().append("td"));
        cells.text(this.raw
            ? fmt.id
            : (d, i) => this.fmt[i](d));
    }
}

LogTable.baseFmt = [fmt.num(10), fmt.num(10), fmt.hex];

LogTable.mungeActionFmt = (action) => action.replace(
    /([@+-])0x([0-9a-fA-F]+)/,
    (_m, sign, str) => sign + fmt.hex(parseInt(str, 16)));

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
            .attr("href", (idi) => `#${idi}`)
            .text((idi) => idi);
    }
}

class Page {
    constructor(chartEl, trailEl, logEl, linksEl) {
        this.chart = new SunburstChart(chartEl);
        this.trail = new SunburstTrail(trailEl);
        this.log = new LogTable(logEl);
        this.links = new Links(thel(linksEl));
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
        if (!parts.length) return;
        let id = parts[0];
        if (!this.model.byID.has(id)) return;
        let path = this.model.findPath(id);
        if (path === null) {
            window.location.hash = "";
            return;
        }
        this.model.cur = path;
        this.showLog(path[path.length-1].data, parts.slice(1));
    }

    load(data) {
        (data instanceof Promise ? data : Promise.resolve(data)
        ).then((data) => {
            this.model = new SunburstModel(data);
            this.trail.model = this.model;
            this.chart.model = this.model;
            this.log.model = this.model;
            this.links.model = this.model;
            this.size();
            this.nav();
        });
    }

    size() {
        this.chart.size();
    }
}

let pg = new Page("#chart", "#sequence", "#log", "#links");
window.addEventListener("resize", () => pg.size());
window.addEventListener("hashchange", () => pg.nav());
pg.load(window[document.querySelector("script.main").dataset.var]);
