"use strict";

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

import {
    hierarchy as d3Hierarchy,
    partition as d3Partition
} from "d3-hierarchy";
import {select as d3Select} from "d3-selection";
import {arc as d3Arc} from "d3-shape";

const midPat = /^(\d+)\((\d+):(\d+)\)$/;
const numColors = 4;

class SunburstModel extends EventEmitter {
    constructor(records) {
        super();
        this.records = records;
        this.byID = {};
        this.kids = {};
        this.rootID = null;
        this.results = new Map();
        this.root = null;
        this._cur = null;

        let resultIDs = [];
        this.records.forEach(d => {
            if (d.parent_id === null) {
                if (this.rootID !== null) {
                    throw new Error("only one root supported");
                }
                this.rootID = d.id;
            }
            let idm = midPat.exec(d.id);
            d.idi = parseInt(idm && idm[3]);
            if (d.error === "" && d.values !== "") {
                resultIDs.push(d.id);
            }
            this.byID[d.id] = d;
            if (d.parent_id !== null) {
                let children = this.kids[d.parent_id];
                if (!children) {
                    children = [];
                    this.kids[d.parent_id] = children;
                }
                children.push(d.id);
            }
        });

        resultIDs.forEach((resultID) => {
            let node = this.byID[resultID];
            let ridi = node.idi;
            for (; node; node = this.byID[node.parent_id]) {
                this.results.set(node.idi, ridi);
            }
        });

        this.root = d3Hierarchy(this.byID[this.rootID], ({id}) => {
            return this.kids[id] && this.kids[id].map((cid) => this.byID[cid]);
        })
            .sum(() => 1)
            .sort(({data: {idi: a}}, {data: {idi: b}}) => a - b);
        // .sort(({value: a}, {value: b}) => a - b);
    }

    get cur() {
        return this._cur;
    }
    set cur(cur) {
        this._cur = cur;
        this.emit("curChanged", cur);
    }
}

class SunburstChart extends EventEmitter {
    constructor(el) {
        super();
        this.el = el;
        this.sel = d3Select(this.el);
        this.partition = d3Partition();
        this.cont = this.sel.append("g");
        this.bound = this.cont.append("circle").attr("id", "bound");
        this.arc = d3Arc()
            .startAngle(({x0}) => x0)
            .endAngle(({x1}) => x1)
            .innerRadius(({y0}) => Math.sqrt(y0))
            .outerRadius(({y1}) => Math.sqrt(y1));
        this.cont.on("mouseleave", () => this.mouseleave());
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
        this.cont.on("mouseleave", (d) => this.mouseleave(d));
        if (this.path) this.path.on("mouseover", (d) => this.mouseover(d));
    }

    deactivate() {
        this.active = false;
        this.cont.on("mouseleave", null);
        if (this.path) this.path.on("mouseover", null);
    }

    mouseover(d) {
        this._model.cur = d && d.ancestors().reverse();
        this.sel.classed("focusing", true);
        this.path.classed("focus", (node) => this._model.cur.indexOf(node) >= 0);
    }

    mouseleave() {
        this._model.cur = null;
        this.sel.classed("focusing", false);
        this.sel.selectAll("path").classed("focus", false);
    }

    clicked(d) {
        this._model.cur = d && d.ancestors().reverse();
        this.emit("nodeActivated", d.data);
    }

    size() {
        const width = document.body.clientWidth;
        const height = document.body.clientHeight;
        const radius = Math.min(width, height) / 2;
        this.partition.size([2 * Math.PI, radius * radius]);
        this.sel
            .attr("width", width)
            .attr("height", height);
        this.cont.attr("transform", `translate(${width/2},${height/2})`);
        this.bound.attr("r", radius);
        if (this._model !== null) this.draw();
    }

    draw() {
        this.path = this.cont
            .data([this._model.records])
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
            .attr("class", ({depth, data: {idi}}) => {
                let parts = [`fillColor${depth % numColors + 1}`];
                if (this._model.results.has(idi)) {
                    parts.push(this._model.results.get(idi) === idi ? "goal" : "goalPath");
                }
                return parts.join(" ");
            });
    }
}

class SunburstTrail {
    constructor(el) {
        this.el = el;
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
            .attr("class", ({depth}) => `bgColor${depth % numColors + 1}`)
            .on("click", this.activationCallback)
        ).text(({data}) => data.idi);
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

class LogTable {
    constructor(el) {
        this.el = el;
        this.sel = d3Select(this.el);
        this.model = null;
    }

    focus(i) {
        this.el.tBodies[i].scrollIntoView();
    }

    show(node) {
        let que = [];
        while (node.parent_id !== null) {
            que.unshift(node);
            node = this.model.byID[node.parent_id];
        }
        que.unshift(node);

        let bodies = this.sel.selectAll("tbody").data(que);
        bodies = bodies.merge(bodies.enter().append("tbody"));

        let rows = bodies.selectAll("tr")
            .data(({id, records}, j) => {
                const next = que[j+1];
                if (next) {
                    for (let i = 0; i < records.length; i++) {
                        if (records[i].kind === "copy" &&
                            records[i].extra["child"] === next.id) {
                            records = records.slice(0, i);
                            break;
                        }
                    }
                }
                return records.map(r => {
                    let idm = midPat.exec(id);
                    return Object.assign({mid: idm[3], depth: j}, r);
                });
            });
        rows = rows.merge(rows.enter().append("tr"));
        rows.attr("class", ({depth}) => `bgColor${depth % numColors + 1}`);

        let cells = rows.selectAll("td")
            .data(({mid, action, count, ip, extra}) => [
                mid, action, count, ip,
                Object.entries(extra).map(([k, v]) => `${k}=${v}`).join(" ")]);
        cells = cells.merge(cells.enter().append("td"));
        cells.text(i => i);
    }
}

class Page {
    constructor(chartEl, trailEl, logEl) {
        this.chart = new SunburstChart(chartEl);
        this.trail = new SunburstTrail(trailEl);
        this.log = new LogTable(logEl);
        this.handleLogKeyUp = (e) => { if (e.keyCode == 27) this.showChart(); };
        this.chart.addListener("nodeActivated", (node) => this.showLog(node));
    }

    showChart() {
        this.chart.el.style.display = "";
        this.log.el.style.display = "none";
        this.trail.deactivate();
        this.chart.activate();
        window.removeEventListener("keyup", this.handleLogKeyUp);
    }

    showLog(node) {
        this.chart.el.style.display = "none";
        this.log.el.style.display = "";
        this.chart.deactivate();
        this.trail.activate((_, i) => this.log.focus(i));
        window.addEventListener("keyup", this.handleLogKeyUp);
        this.log.show(node);
    }

    load(data) {
        let model = new SunburstModel(data);
        this.trail.model = model;
        this.chart.model = model;
        this.log.model = model;
        this.size();
    }

    size() {
        this.chart.size();
    }
}

let pg = new Page(document.querySelector("#chart"), document.querySelector("#sequence"), document.querySelector("#log"));
window.addEventListener("resize", () => pg.size());
pg.size();

const mainScript = document.querySelector("script.main");
if (mainScript) {
    let dataVar = mainScript.dataset.var;
    if (dataVar) {
        let dat = window[dataVar];
        if (!(dat instanceof Promise)) dat = Promise.resolve(dat);
        dat.then((data) => pg.load(data));
    }
}
