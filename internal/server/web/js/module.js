const addOptionsToElement = (el, opts, selected) => {
  for (let i = 0; i < opts.length; i++) {
    const opt = opts[i];

    let option = document.createElement("option");
    option.label = opt;
    option.value = opt;
    if (opt === selected) {
      option.selected = "selected";
    }
    el.append(option);
  }
};

const params = new URLSearchParams(window.location.search);
const module = params.get("id");
let version = params.get("version");
let direction = params.get("direction");

// Set the page title
document.title = module;
document.getElementById("title").innerHTML = module;

async function loadPage() {
  // Fetch the module's versions
  const versions = await getModuleVersions(module);
  if (versions.length === 0) {
    alert(`no versions found for ${module}`);
  }

  // Set defaults
  if (version == null) {
    version = versions[0];
  }
  if (direction == null) {
    direction = "dependents";
  }

  // Populate select dropdowns
  const versionSelect = document.getElementById("version");
  versionSelect.addEventListener("change", function () {
    window.location.href = `/ui/module.html?id=${module}&version=${this.value}&direction=${direction}`;
  });
  addOptionsToElement(versionSelect, versions, version);

  const directionSelect = document.getElementById("direction");
  directionSelect.addEventListener("change", function () {
    window.location.href = `/ui/module.html?id=${module}&version=${version}&direction=${this.value}`;
  });
  addOptionsToElement(
    directionSelect,
    ["dependencies", "dependents"],
    direction
  );

  // Fetch the module@version's first-level dependencies and render the graph
  const { nodes, links } = await getModuleDeps(module, version, direction);
  const onClick = function (node) {
    const [module, version] = node.id.split("@");
    window.location.href = `/ui/module.html?id=${module}&version=${version}&direction=${direction}`;
  };

  RenderGraph(nodes, links, onClick);
}

loadPage();
