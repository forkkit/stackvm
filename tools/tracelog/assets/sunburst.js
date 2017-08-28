"use strict";

import {
    hierarchy as d3Hierarchy,
    partition as d3Partition
} from "d3-hierarchy";
import {select as d3Select} from "d3-selection";
import {arc as d3Arc} from "d3-shape";

const midPat = /^(\d+)\((\d+):(\d+)\)$/;
const numColors = 4;

class SunburstModel {
    constructor(records) {
        this.records = records;
        this.byID = {};
        this.kids = {};
        this.rootID = null;
        this.results = new Map();
        this.root = null;
        this.cur = null;

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
}

let model = null;

const chart = document.querySelector("#chart");
const trail = document.querySelector("#sequence");
const log = document.querySelector("#log");

const partition = d3Partition();
const cont = d3Select(chart).append("g");
const bound = cont.append("circle").attr("id", "bound");

const arc = d3Arc()
    .startAngle(({x0}) => x0)
    .endAngle(({x1}) => x1)
    .innerRadius(({y0}) => Math.sqrt(y0))
    .outerRadius(({y1}) => Math.sqrt(y1));

cont.on("mouseleave", mouseleave);

window.addEventListener("resize", updateSize);
updateSize();

const mainScript = document.querySelector("script.main");
if (mainScript) {
    let dataVar = mainScript.dataset.var;
    if (dataVar) {
        let dat = window[dataVar];
        if (!(dat instanceof Promise)) dat = Promise.resolve(dat);
        dat.then(load);
    }
}

function updateSize() {
    let width = document.body.clientWidth;
    let height = document.body.clientHeight;
    let radius = Math.min(width, height) / 2;
    partition.size([2 * Math.PI, radius * radius]);
    d3Select(chart)
        .attr("width", width)
        .attr("height", height);
    cont.attr("transform", `translate(${width/2},${height/2})`);
    bound.attr("r", radius);
    if (model !== null) draw();
}

function load(data) {
    model = new SunburstModel(data);
    updateSize();
}

function draw() {
    let path = cont
        .data([model.records])
        .selectAll("path")
        .data(partition(model.root).descendants());

    path = path.merge(path
        .enter().append("path")
        .attr("fill-rule", "evenodd")
        .on("mouseover", mouseover)
        .on("click", clicked)
    );

    path
        .attr("display", ({depth}) => depth ? null : "none")
        .attr("d", arc)
        .attr("class", ({depth, data: {idi}}) => {
            let parts = [`fillColor${depth % numColors + 1}`];
            if (model.results.has(idi)) {
                parts.push(model.results.get(idi) === idi ? "goal" : "goalPath");
            }
            return parts.join(" ");
        });

    if (model && model.cur) updateBreadcrumbs(model.cur);
}

window.addEventListener("keyup", (e) => {
    if (e.keyCode == 27) hideLog();
});

function showLog(node) {
    cont.selectAll("path").on("mouseover", null);
    cont.on("mouseleave", null);
    chart.style.display = "none";
    log.style.display = "";
    trail.className = "active";
    d3Select(trail).selectAll("li").on("click", (_, i) => {
        log.tBodies[i].scrollIntoView();
    });

    let ids = model.cur.map(({data: {id}}) => id);

    let que = [];
    while (node.parent_id !== null) {
        que.unshift(node);
        node = model.byID[node.parent_id];
    }
    que.unshift(node);

    let sel = d3Select(log).selectAll("tbody").data(que);
    sel = sel.merge(sel.enter().append("tbody"));

    sel = sel.selectAll("tr")
        .data(({id, records}, j) => {
            let nextID = ids[j+1];
            if (nextID) {
                for (let i = 0; i < records.length; i++) {
                    if (records[i].kind === "copy" &&
                        records[i].extra["child"] === nextID) {
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
    sel = sel.merge(sel.enter().append("tr"));
    sel.attr("class", ({depth}) => `bgColor${depth % numColors + 1}`);

    sel = sel.selectAll("td")
        .data(({mid, action, count, ip, extra}) => [
            mid, action, count, ip,
            Object.entries(extra).map(([k, v]) => `${k}=${v}`).join(" ")]);
    sel = sel.merge(sel.enter().append("td"));
    sel.text(i => i);
}

function hideLog() {
    cont.selectAll("path").on("mouseover", mouseover);
    cont.on("mouseleave", mouseleave);
    chart.style.display = "";
    log.style.display = "none";
    trail.className = "";
    d3Select(trail).selectAll("li").on("click", null);
}

function clicked(d) {
    updateBreadcrumbs(d);
    showLog(d.data);
}

function mouseover(d) {
    updateBreadcrumbs(d);
    d3Select(chart)
        .classed("focusing", true);
    cont.selectAll("path")
        .classed("focus", (node) => model.cur.indexOf(node) >= 0);
}

function mouseleave() {
    const sel = d3Select(chart);
    sel.classed("focusing", false);
    sel.selectAll("path").classed("focus", false);
    updateBreadcrumbs(null);
}

function updateBreadcrumbs(d) {
    if (!model) return;
    model.cur = d && d.ancestors().reverse();
    let items = d3Select(trail)
        .selectAll("li")
        .data(model.cur || [], ({data, depth}) => data.id + depth);
    items.exit().remove();
    items.merge(items.enter()
        .append("li")
        .attr("class", ({depth}) => `bgColor${depth % numColors + 1}`)
    )
        .text(({data}) => data.idi);
}


