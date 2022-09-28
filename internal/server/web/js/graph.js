// Adapted from:
// https://github.com/ninjaconcept/d3-force-directed-graph/tree/master/example

const RenderGraph = function (nodes, links, onClickNode) {
  const width = window.innerWidth / 2,
    height = window.innerHeight / 1.5,
    nodeRadius = 30,
    linkLength = 300,
    linkStrength = 1;

  const svg = d3.select("svg");

  function getNodeColor(node, neighbors) {
    return node.level === 0 ? "red" : "gray";
  }

  function getTextColor(node, neighbors) {
    return "black";
  }

  var linkForce = d3
    .forceLink()
    .id(function (link) {
      return link.id;
    })
    .distance(function (link) {
      return link.length || linkLength;
    })
    .strength(function (link) {
      return link.strength || linkStrength;
    });

  var simulation = d3
    .forceSimulation()
    .force("link", linkForce)
    .force("charge", d3.forceManyBody().strength(-120))
    .force("center", d3.forceCenter(width / 2, height / 2));

  var dragDrop = d3
    .drag()
    .on("start", function (node) {
      node.fx = node.x;
      node.fy = node.y;
    })
    .on("drag", function (node) {
      simulation.alphaTarget(0.7).restart();
      node.fx = d3.event.x;
      node.fy = d3.event.y;
    })
    .on("end", function (node) {
      if (!d3.event.active) {
        simulation.alphaTarget(0);
      }
      node.fx = null;
      node.fy = null;
    });

  var linkElements = svg
    .append("g")
    .attr("class", "links")
    .selectAll("line")
    .data(links)
    .enter()
    .append("line")
    .attr("stroke-width", 1)
    .attr("stroke", "rgba(50, 50, 50, 0.2)");

  var nodeElements = svg
    .append("g")
    .attr("class", "nodes")
    .selectAll("circle")
    .data(nodes)
    .enter()
    .append("circle")
    .attr("r", nodeRadius)
    .attr("fill", getNodeColor)
    .call(dragDrop)
    .on("mouseover", function () {
      d3.select(this).style("cursor", "pointer");
    })
    .on("mouseout", function () {
      d3.select(this).style("cursor", "default");
    })
    .on("click", onClickNode);

  var textElements = svg
    .append("g")
    .attr("class", "texts")
    .selectAll("text")
    .data(nodes)
    .enter()
    .append("text")
    .text(function (node) {
      return node.label;
    })
    .attr("font-size", 15)
    .attr("dx", 15)
    .attr("dy", 4);

  simulation.nodes(nodes).on("tick", () => {
    nodeElements
      .attr("cx", function (node) {
        return node.x;
      })
      .attr("cy", function (node) {
        return node.y;
      });
    textElements
      .attr("x", function (node) {
        return node.x;
      })
      .attr("y", function (node) {
        return node.y;
      });
    linkElements
      .attr("x1", function (link) {
        return link.source.x;
      })
      .attr("y1", function (link) {
        return link.source.y;
      })
      .attr("x2", function (link) {
        return link.target.x;
      })
      .attr("y2", function (link) {
        return link.target.y;
      });
  });

  simulation.force("link").links(links);
};
