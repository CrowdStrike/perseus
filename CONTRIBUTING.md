# Contributing

_Welcome!_ We're excited you want to take part in the CrowdStrike community!

Please review this document for details regarding getting started with your first contribution, tools
you'll need to install as a developer, and our development and Pull Request process. If you have any
questions, please let us know by posting your question in the [discussion board](https://github.com/CrowdStrike/perseus/discussions).

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [How you can contribute](#how-you-can-contribute)
- [Working with the Code](#working-with-the-code)
  - [Prerequisites](#prerequisites)
  - [Other Build Tools](#other-build-time-tools)
  - [Commit Messages](#commit-message-formatting-and-hygiene)
- [Pull Requests](#pull-requests)
  - [Code Coverage](#code-coverage)

## Code of Conduct

Please refer to CrowdStrike's general [Code of Conduct](https://opensource.crowdstrike.com/code-of-conduct/)
and [contribution guidelines](https://opensource.crowdstrike.com/contributing/).

## How you can contribute

- See something? Say something! Submit a [bug report](https://github.com/CrowdStrike/perseus/issues/new?assignees=&labels=bug%2Ctriage&template=bug.md&title=) to let the community know what you've experienced or found.
  - Please propose new features on the discussion board first.
- Join the [discussion board](https://github.com/CrowdStrike/perseus/discussions) where you can:
  - [Interact](https://github.com/CrowdStrike/perseus/discussions/categories/general) with other members of the community
  - [Start a discussion](https://github.com/CrowdStrike/perseus/discussions/categories/ideas) or submit a [feature request](https://github.com/CrowdStrike/perseus/issues/new?assignees=&labels=enhancement%2Ctriage&template=feature_request.md&title=)
  - Provide [feedback](https://github.com/CrowdStrike/perseus/discussions/categories/q-a)
  - [Show others](https://github.com/CrowdStrike/perseus/discussions/categories/show-and-tell) how you are using `perseus` today
- Submit a [Pull Request](#pull-requests)

## Working with the Code

To simplify and standardize things, we use GNU Make to execute local development tasks like building
and linting the code, running tests and benchmarks, etc.

The table below shows the most common targets:

| Target       | Description                                                                          |
| ------------ | ------------------------------------------------------------------------------------ |
| `test`       | Runs all tests for the module                                                        |
| `lint`       | Runs the `golangci-lint` linter against the project                                  |
| `protos`     | Generates the Go code in the `perseusapi` sub-package by invoking the Buf CLI        |
| `bin`        | Builds the `perseus` binary and writes the result to the project-level `bin/` folder |
| `install`    | Builds the `perseus` binary and writes the Go install folder                         |
| `snapshot`   | Invokes `goreleaser` to generate the configured artifacts                            |

### Prerequisites

We have tried to minimize the "extra" work you'll have to do, but there are a few prerequisites:

- GNU Make
- Go 1.18 or higher
- Docker
  - We use Docker Compose to spin up a local PostgreSQL container for development
- The `buf` Protobuf compiler from Buf ([link](https://buf.build))

As the exact details of how to install these tools varies from system to system, we have chosen to
leave that part to you.

### Other Build-Time Tools

All other build-time tooling is installed into a project-level `bin/` folder because we don't want to impact
any other projects by overwriting binaries in `$GOPATH/bin` (or `$GOROOT/bin`). To accomodate that, `Makefile`
prepends the local `bin/` folder to `$PATH` so that any Make targets will find the build-time tools there. If
you choose to not use our Make targets, please ensure that you adjust your environment accordingly.

### Commit Message Formatting and Hygiene

We use [_Conventional Commits_](https://www.conventionalcommits.org/en/v1.0.0/) formatting for commit
messages, which we feel leads to a much more informative change history. Please familiarize yourself
with that specification and format your commit messages accordingly.

Another aspect of achieving a clean, informative commit history is to avoid "noise" in commits.
Ideally, condense your changes to a single commit with a well-written _Conventional Commits_ message
before submitting a PR. In the rare case that a single PR is introducing more than one change, each
change should be a single commit with its own well-written message.

## Pull Requests

All code changes should be submitted via a Pull Request targeting the `main` branch. We are not assuming
that every merged PR creates a release, so we will not be automatically creating new SemVer tags as
a side effect of merging your Pull Request. Instead, we will manually tag new releases when required.

### Code Coverage

While we feel like achieving and maintaining 100% code coverage is often an untenable goal with
diminishing returns, any changes that reduce code coverage will receive pushback. We don't want
people to spend days trying to bump coverage from 97% to 98%, often at the expense of code clarity,
but that doesn't mean that we're okay with making things worse.

## Creating a Local Database

As mentioned above, we use Docker Compose to streamline the process of standing up a local database
for testing.  There are only a few steps.

0) Optionally, edit `docker-compose.yml` to specify a different default user and/or password for PostgreSQL
1) Run `docker-compose up` from the root folder to create and start the PostreSQL container
2) Using the tool of your choice, connect to the empty `perseus` database at `localhost:5432` and
   run the creation script at `internal/store/create_database.sql`

That's it. You now have a local Perseus database ready to populate with all of your Go module dependencies.
