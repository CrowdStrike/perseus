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
    .then((resp) => resp.versions);
};

const getModuleDeps = (module, version, direction) => {
  return fetch(
    `${apiBase}/modules-dependencies?module_name=${module}&version=${version}&direction=${direction}`
  )
    .then((data) => data.json())
    .then((resp) => {
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
