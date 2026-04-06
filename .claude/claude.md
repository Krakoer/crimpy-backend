# Crimpy Development Guide for AI Agents

This is a Golang app, using postgresql as database, sqlc for code generation, atlas for migrations and Fiber as an HTTP framework. It runs using docker-compose for production and dev environnements, and uses air for hot reloading in the dev env. A Justfile is available to provide quick commands.

Crimpy is a climbing training project composed of a flutter application connected to a BLE force sensor, and a backend. This part is the bakend of the project. 

## Initial Setup (already done)

Before you start, the following command was already run for you: `just up` set the dev env up.

## Commands useful in development

Always run all commands using the zsh shell.

- `just make_migration <migration_name>` creates a migration after modification of the schema.
- `just migrate` apply the migrations to the dev env.
- `just migrate` apply the migrations to the dev env.
- `just logs` prints the containers logs.

## Linting and formatting

Human devs have IDEs that autoformat code on every file save. After you edit files, you must do the equivalent by running `go fmt`.

## Testing

Not implemented yet.

## Interacting with the app

To interact with the running dev env exposed by docker on port 3000, you can use the Bruno CLI tool using the `bru` command, see .claude/bruno.md file for more instructions. You can create Bruno files for the different APIs to test.

## Debugging

You can view the web app logs using `just logs`.

## Patterns

We value code that explains itself through clear class, method, and variable names. Comments may be used when necessary to explain some tricky logic, but should otherwise be avoided.
