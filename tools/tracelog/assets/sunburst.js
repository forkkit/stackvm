"use strict";

import {
    hierarchy as d3Hierarchy,
    partition as d3Partition
} from "d3-hierarchy";
import {select as d3Select} from "d3-selection";
import {arc as d3Arc} from "d3-shape";

const midPat = /^(\d+)\((\d+):(\d+)\)$/;
const numColors = 4;

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
    let rootID = null;
    let byID = {};
    let kids = {};
    let results = [];
    data.forEach(d => {
        if (d.parent_id === null) {
            if (rootID !== null) {
                throw new Error("only one root supported");
            }
            rootID = d.id;
        }
        let idm = midPat.exec(d.id);
        d.idi = parseInt(idm && idm[3]);
        if (d.error === "" && d.values !== "") {
            results.push(d.id);
        }

        byID[d.id] = d;
        if (d.parent_id !== null) {
            let children = kids[d.parent_id];
            if (!children) {
                children = [];
                kids[d.parent_id] = children;
            }
            children.push(d.id);
        }
    });

    // TODO: complete results so that all parents are marked
    // let n = results.length;

    model = {
        recods: data,
        byID: byID,
        kids: kids,
        rootID: rootID,
        results: results,
        root: null,
        cur: null
    };

    model.root = d3Hierarchy(model.byID[rootID], ({id}) => {
        return kids[id] && kids[id].map((cid) => byID[cid]);
    })
        .sum(() => 1)
        .sort(({data: {idi: a}}, {data: {idi: b}}) => a - b);
    // .sort(({value: a}, {value: b}) => a - b);

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
        .style("opacity", 1)
        .on("mouseover", mouseover)
        .on("click", clicked)
    );

    path
        .attr("display", ({depth}) => depth ? null : "none")
        .attr("d", arc)
        .attr("class", ({depth}) => `fillColor${depth % numColors + 1}`);

    if (model && model.cur) updateBreadcrumbs(model.cur);
}

window.addEventListener("keyup", (e) => {
    if (e.keyCode == 27) hideLog();
});

function showLog() {
    cont.selectAll("path").on("mouseover", null);
    cont.on("mouseleave", null);
    chart.style.display = "none";
    log.style.display = "";
    trail.className = "active";
    d3Select(trail).selectAll("li").on("click", (_, i) => {
        log.tBodies[i].scrollIntoView();
    });
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
    showLog();

    updateBreadcrumbs(d);
    let ids = model.cur.map(({data: {id}}) => id);

    let data = d.data;
    let que = [];
    while (data.parent_id !== null) {
        que.unshift(data);
        data = model.byID[data.parent_id];
    }
    que.unshift(data);

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

function mouseover(d) {
    updateBreadcrumbs(d);
    cont.selectAll("path")
        .style("opacity", 0.3);
    cont.selectAll("path")
        .filter((node) => model.cur.indexOf(node) >= 0)
        .style("opacity", 1);
}

function mouseleave() {
    d3Select(chart).selectAll("path").style("opacity", 1);
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


