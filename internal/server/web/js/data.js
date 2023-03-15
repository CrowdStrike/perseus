const apiBase = "/api/v1";

const listModules = async () => {
  return fetch(`${apiBase}/modules`)
    .then((data) => data.json())
    .then((resp) => resp.modules);
};

const getModuleVersions = (module) => {
  return fetch(
    `${apiBase}/module-versions?module_name=${module}&version_option=all`
    )
    .then((data) => data.json())
    .then((resp) => {
      // API response structure is 1-element array of modules
      //  {"modules":[{"name": "github.com/example/foo", "versions":["v0.1.0", "v0.2.0", ...]}]}
      //
      return resp.modules[0].versions
    });
};

const getModuleDeps = (module, version, direction) => {
  return fetch(
    `${apiBase}/modules-dependencies?module_name=${module}&version=${version}&direction=${direction}`
  )
    .then((data) => data.json())
    .then((resp) => {
      // API response structure is an array of modules that are direct dependencies/dependants of
      // the current module, each with a single version
      //  {"modules":[{"name": "github.com/example/foo", "versions":["v0.1.0"]}, ...]}
      //
      const fqmn = `${module}@${version}`;

      let out = { nodes: [], links: [] };
      out.nodes.push({ id: fqmn, label: fqmn, level: 0 });

      resp.modules.forEach((mod) => {
        const name = `${mod.name}@${mod.versions[0]}`;
        out.nodes.push({ id: name, label: name, level: 1 });
        out.links.push({ source: fqmn, target: name });
      });

      return out;
    });
};
