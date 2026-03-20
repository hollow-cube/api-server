# API Server

The backend service for [Hollow Cube](https://hollowcube.net) — a Minecraft server for creative map building.
This accompanies the [mapmaker](https://github.com/hollow-cube/mapmaker) game server repo.

## Project Structure

- `api/`
    - `v1Public/` - Current-version public API for game data
    - `v4Internal/` - Current-version API for the game servers
    - `external/` - Incoming webhook endpoints for external services (eg Discord, Tebex)
    - `posthog/` - Caching PostHog feature flag proxy
    - `mapsV3/` - (deprecated) `map-service` api, in-progress migration to `v4Internal`
    - `v2/` - (deprecated) `player-service` api, in-progress migration to `v4Internal`
    - `v3/` - (deprecated) `session-service` api, in-progress migration to `v4Internal`
    - `auth/` - (deprecated) Envoy auth middleware

*Note: Historically this project was made up of 3 distinct services (`map-service`, `player-service`,
`session-service`) so you may see references to those. They have since been merged but there remains
some legacy separation.*

## Getting Started

See [Development Setup](https://github.com/hollow-cube/map-maker/blob/main/.github/DEVELOPMENT_SETUP.md) for
instructions on running the project locally.

## Contributing

Please read [CONTRIBUTING.md](.github/CONTRIBUTING.md) before opening a pull request.

All contributors must sign our
[Contributor License Agreement](https://hollowcube.net/legal/individual-contributor-license-agreement).
You'll be prompted automatically on your first PR.

## Community

We have a dedicated `#general-dev` channel in our [Discord](https://discord.hollowcube.net) for related questions.

## License

The code in this repository is licensed under the [MIT License](LICENSE).
