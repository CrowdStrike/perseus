const addModulesToElement = (el, modules) => {
  modules.forEach((mod) => {
    let a = document.createElement("a");
    a.href = `/ui/module.html?id=${mod.name}`;
    a.innerHTML = `${mod.name}`;

    let div = document.createElement("div");
    div.append(a);

    el.append(div);
  });
};

const fuzzySearch = function (string, term, ratio) {
  string = string.toLowerCase();
  let compare = term.toLowerCase();
  let matches = 0;
  if (string.indexOf(compare) > -1) return true; // covers basic partial matches
  for (let i = 0; i < compare.length; i++) {
    string.indexOf(compare[i]) > -1 ? (matches += 1) : (matches -= 1);
  }
  return matches / string.length >= ratio || term == "";
};

async function loadPage() {
  const content = document.getElementById("content");
  const count = document.getElementById("count");

  // Populate the page with modules
  const modules = await listModules();
  count.innerHTML = `(${modules.length})`;
  addModulesToElement(content, modules);

  // Filter the modules based on user input
  const filter = document.getElementById("search");
  filter.addEventListener("keyup", function () {
    const input = this.value;
    const filteredMods = modules.filter((mod) =>
      fuzzySearch(mod.name, input, 0.5)
    );
    content.innerHTML = "";
    addModulesToElement(content, filteredMods);
  });
}

loadPage();
