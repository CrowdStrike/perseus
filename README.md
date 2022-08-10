# Perseus

[![GoDoc](https://pkg.go.dev/badge/github.com/CrowdStrike/perseus.svg)](https://pkg.go.dev/github.com/CrowdStrike/perseus)

With the introduction of Go modules, our projects' dependency trees have exploded in complexity.  Perseus
is the hero we need to battle that Kraken.

## Overview

At CrowdStrike, the move to Go Modules from our existing GOPATH-mode monorepo has brought with it some
pain points, especially around tracing descendant dependencies.  In the GOPATH days, when our monorepo
lived under `$GOPATH/src` and all engineers always had a full copy of the entire codebase, it was a
straightforward `grep` command to find all imports of a given package to see what other packages depend on it.

Now that we've moved the majority of our development effort to modules, neither of those two conditions
are true.  Engineers are no longer required to have the code under `$GOPATH/src` and almost no one has
the entire codebase locally anymore.  We have dozens of functional teams working on hundreds of microservices,
so most developers have pared down their local workspace to only the things they're directly working
on on a day-to-day basis.

An unfortunate side effect of this paradigm shift has been that there is no longer a direct way to see
which other modules depend on your work.  Existing tools like `go mod graph` and `go list -m all` will
show you what modules you depend on - with some rough edges - and the `pkg.go.dev` site has an _Imported By_
view that shows what other packages depend on your code.  The `go` tool won't show which things depend
on you, though.  The `pkg.go.dev` site can only show things that it knows about, so it won't help for
private modules, and it doesn't show you which versions of those other packages depend on your code.

## Existing Tooling

Unfortunately, the `go` CLI commands, the `pkg.go.dev` site, and OSS tools like [`goda`](https://github.com/loov/goda)
and [`godepgraph`](https://github.com/kisielk/godepgraph) don't quite cover the ground we need, specifically
querying for downstream dependencies.  The `go` CLI, `goda` and `godepgraph` all do an excellent job
of surfacing up which modules your code depends on in multiple ways.  The pkg.go.dev site also provides
a nice _Imported By_ view, but it shows which packages depend on your code, not which modules, and
doesn't include the **version(s)** of those dependents.

### Perseus To The Rescue

#### The Database (5 minutes)

For simplicity, Perseus uses a PostgreSQL database rather than an actual graph database like Neo4J,
Cayley, and the like.  After some initial investigation, we found that the relatively small number of
entries (compared to other graph datasets) didn't warrant a specialized graph database.  Additionally,
our IT organization already has all of the necessary infrastructure in place to support PostgreSQL,
both in-house and hosted in AWS.

#### The Service

##### Service Architecture

The `perseus` service exposes a gRPC API, along with JSON/REST mappings using the [gRPC Gateway](https://github.com/grpc-ecosystem/grpc-gateway)
project for exposure to web-based consumers.  Both endpoints, plus a very basic web UI for testing,
are served on a single port using [cmux](https://github.com/soheilhy/cmux).

##### Service Operations

Updates to the graph are made by submitting a request with the name and version of a module, along
with a list of its dependencies (name and version).  This request is converted into a set of "edges"
in the graph linking the name/version pairs.  Since tagged module versions are frozen in Go, any
existing data for the submitted module is first removed.

Example update:

    curl --request POST http://localhost/api/v1/modules/github.com/example/foo@v1.2.3/versions/v1.2.3/deps
       --header 'Content-Type: application/json'
       --data-raw '{"dependencies":[{"name":"github.com/rs/zerolog","versions:["v1.27.0"]}, {"name":"golang.org/x/text","versions":["v0.0.0-...."]}'

Queries consist of a target module, an optional version, and a direction (ancestors or descendants).
An "ancestor" query returns the set of modules that the target module depends on and a "descendant"
query returns the set of modules that depend on the target.  In either case, if no version is specified
for the target, the most recent known version is used.

Example query:

    curl --request GET http://localhost/api/graph-query?module=github.com%2Fexample%2Ffoo%40v1.2.3&mode=descendant

The format of the response is either:

- a JSON document containing the graph of dependencies as nested objects
- a DOT file describing the graph
- a rendered PNG or SVG image of the graph

#### Updating the Graph

The `perseus` binary is both the server and a CLI tool for interacting with it.  When used as a CLI, `perseus update ...` extracts the direct dependencies of a specified Go module and updates the Perseus graph.  By scripting executions of `perseus update` across the codebase, including as a CI step when new module versions are released, the Perseus graph can be incrementally grown to include all of the information necessary to query both ancestors and descendants of any particular version of a module.

_Disclaimer: `perseus` is an open source project, not a CrowdStrike product. As such, it carries no
formal support, expressed or implied.  The project is licensed under the MIT open source license._
