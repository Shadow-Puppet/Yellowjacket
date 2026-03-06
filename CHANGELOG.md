## [1.2.2](https://github.com/onion-4-dinner/yellowjacket/compare/v1.2.1...v1.2.2) (2026-03-06)

### Bug Fixes

* recover from go-mp3 seek panic on startup ([#86](https://github.com/onion-4-dinner/yellowjacket/issues/86)) ([2f9d9f8](https://github.com/onion-4-dinner/yellowjacket/commit/2f9d9f8508b90b6188fe894c282c5b8e330e8046))

## [1.2.1](https://github.com/onion-4-dinner/yellowjacket/compare/v1.2.0...v1.2.1) (2026-03-06)

### Bug Fixes

* **deps:** pin go-webview2 to v1.0.21 for Wails v2 compat ([25f0fe8](https://github.com/onion-4-dinner/yellowjacket/commit/25f0fe81560eeff36a0b2beb52ce1bdf13d5e122))

## [1.2.0](https://github.com/onion-4-dinner/yellowjacket/compare/v1.1.3...v1.2.0) (2026-03-06)

### Features

* **02-02:** add ScanWarning type and reclassify scan errors as warnings ([e6866de](https://github.com/onion-4-dinner/yellowjacket/commit/e6866ded9dc0ea30ff942cd31b6c5ea3269e9584))
* **03-01:** create NewTestDB helper for in-memory SQLite test databases ([bae9d70](https://github.com/onion-4-dinner/yellowjacket/commit/bae9d70d23157ef4e79e60dd713d9a02ab63790b))
* **03-01:** extract shared applyPRAGMAs and add production PRAGMAs to NewDB ([d348815](https://github.com/onion-4-dinner/yellowjacket/commit/d34881530adda7fb75be84737798da46d17bfa8c))
* **06-01:** create track_metadata VIEW schema and migration 4 ([9c7e5a9](https://github.com/onion-4-dinner/yellowjacket/commit/9c7e5a96344a81bf132de487b4763f1dc3ff6df9))
* **06-02:** create Go→TypeScript event constant codegen tool ([3e9edd0](https://github.com/onion-4-dinner/yellowjacket/commit/3e9edd05e87395499ac24e456640d1f6d9b97f04))
* **06-03:** migrate lookupChunk to sqlc-generated LookupTrackMetaByPaths query ([2221a68](https://github.com/onion-4-dinner/yellowjacket/commit/2221a68459850a837c996c6e6d2bc95d41b20fb3))
* **08-01:** define design token CSS custom properties for icon sizes and type scale ([1444a66](https://github.com/onion-4-dinner/yellowjacket/commit/1444a66bb201ce5fdf16552a32bcd281089c64ed))
* **08-04:** apply design tokens to cover-grid, track-list, queue-panel, and detail components ([1303422](https://github.com/onion-4-dinner/yellowjacket/commit/1303422e69c27d528363900b3ca5287a48cc9f8e))
* **08-04:** convert sidebar em-based spacing to px and apply icon/type tokens ([aed90d7](https://github.com/onion-4-dinner/yellowjacket/commit/aed90d7b1710d0c5cece2e4956c0a6ce77b9a999))
* add scan progress bar with phase indicator ([a28b4d1](https://github.com/onion-4-dinner/yellowjacket/commit/a28b4d1e0673658824750d4c702359321dc9a78e))
* **quick-001:** add multi-file picker and batch import support ([c34e4ad](https://github.com/onion-4-dinner/yellowjacket/commit/c34e4ad029c119bff8f70a07ccc6bca58b11ea3c))
* **quick-001:** regenerate bindings and update frontend for multi-import ([2a542bf](https://github.com/onion-4-dinner/yellowjacket/commit/2a542bf3bcdc7772edb1aceb41f488774494f656))
* **quick-002:** add CountPlaylistsByName SQL query and regenerate sqlc ([04b2088](https://github.com/onion-4-dinner/yellowjacket/commit/04b2088b28b84a4d4df25b23d97112c5a955dff1))
* **quick-002:** add uniquePlaylistName helper and wire into ImportPlaylist ([8ba8bbe](https://github.com/onion-4-dinner/yellowjacket/commit/8ba8bbe7bed2ecff97613ebaa42a49a662050353))
* **quick-006:** remove list icon from playlists, add favorites icon to default ([3c19766](https://github.com/onion-4-dinner/yellowjacket/commit/3c19766fd0885d4171cf9929db6d69a3d5c1a3ff))
* **quick-11:** add configurable log level via YJ_LOG_LEVEL env var ([55b4902](https://github.com/onion-4-dinner/yellowjacket/commit/55b4902fac7b7f2c04ad5efac398ecedc5fedc2f))
* **quick-11:** add make dev-debug target for verbose logging ([c45bca4](https://github.com/onion-4-dinner/yellowjacket/commit/c45bca411ba1d4f32deea6027acf91237173dd15))
* **quick-12:** add favorite icon to album dropdown track rows ([12a0bbc](https://github.com/onion-4-dinner/yellowjacket/commit/12a0bbc89c19128485d597a61bd16bd0786450ad))
* **quick-15:** add BufferedStreamer with goroutine read-ahead ([85b23ac](https://github.com/onion-4-dinner/yellowjacket/commit/85b23acb24a048d2f7b85808e477bb991ae124e6))
* **quick-15:** insert BufferedStreamer into player pipeline and increase speaker buffer ([8a0b16a](https://github.com/onion-4-dinner/yellowjacket/commit/8a0b16a4ec08a95bfd3834c8216e21dce854432d))
* **quick-3:** add playlist-level multi-select state and selection handling ([e13151f](https://github.com/onion-4-dinner/yellowjacket/commit/e13151ffa5dc86e41ce242421679d65a740c3af0))
* **quick-3:** wire playlist context menu for batch delete of selected playlists ([c92ced2](https://github.com/onion-4-dinner/yellowjacket/commit/c92ced2c74e72bfc123c880c047462dc969cde34))
* **quick-4:** add 'Set as Default Playlist' context menu option ([9971b63](https://github.com/onion-4-dinner/yellowjacket/commit/9971b635b81fe3f8621c80a6664eccb3e1fc4bb8))
* **quick-5:** add CreatedAt/UpdatedAt to playlist Summary struct ([bdaff47](https://github.com/onion-4-dinner/yellowjacket/commit/bdaff478e802ee5c0745327c52dd9b190fcfef7d))
* **quick-5:** add sort dropdown UI and client-side sorting to playlist view ([5c07485](https://github.com/onion-4-dinner/yellowjacket/commit/5c074855351f1363cc7918837a78bbd3c0b7ebf5))
* **quick-7:** add PinDefault config field with backend getter/setter ([6e123bd](https://github.com/onion-4-dinner/yellowjacket/commit/6e123bd47f55e6d565f20bf7f19950e65f80787f))
* **quick-7:** wire frontend pin-default-playlist feature end-to-end ([e6378e1](https://github.com/onion-4-dinner/yellowjacket/commit/e6378e1f0d3b0f2a7604b8ef6097dba9050cdd16))
* **quick-8:** add FindDuplicateTracksInPlaylist backend method ([83de934](https://github.com/onion-4-dinner/yellowjacket/commit/83de934c39ca7d850a8b5925c90e6d0b3fe0a487))
* **quick-8:** create duplicate-tracks-dialog component ([9f3ba2b](https://github.com/onion-4-dinner/yellowjacket/commit/9f3ba2b9d474fa30dcb4934b01d4650e0d0d3cba))
* **quick-8:** wire duplicate detection into playlist-picker and playlist-view ([917a79a](https://github.com/onion-4-dinner/yellowjacket/commit/917a79a8d6e30dddd2170323bb26692386794872))

### Bug Fixes

* **01-01:** add mutex protection to Queue, Library, and Playlist SetContext methods ([daaa6b7](https://github.com/onion-4-dinner/yellowjacket/commit/daaa6b7f9779385979fe9dddae4e7bb388b3e5fb))
* **01-01:** collapse Player.SetContext double-lock into single acquisition ([3abaeba](https://github.com/onion-4-dinner/yellowjacket/commit/3abaeba3afb0f4d0edb81e26ca55b31bf59990ac))
* **02-01:** eliminate package-level startupErr and fix config file permissions ([2a86408](https://github.com/onion-4-dinner/yellowjacket/commit/2a864082017e489ffa086c136f1002277a77a7c4))
* **02-01:** log MPRIS callback errors instead of discarding them ([0860b2f](https://github.com/onion-4-dinner/yellowjacket/commit/0860b2fd4b2250da1eeb80c21f14fdf341697501))
* **08-02:** revert repeat() inside lit-virtualizer, restore .renderItem + .keyFunction ([72ef719](https://github.com/onion-4-dinner/yellowjacket/commit/72ef719ba70eeca0fa4bae47df092706f6fbaeed))
* drop+recreate contentless FTS5 index instead of DELETE ([8e9a616](https://github.com/onion-4-dinner/yellowjacket/commit/8e9a61603779eacbee7013b9bc760b315baf782a))
* **frontend:** reposition search indicator into toolbar and fix album cover art lookup ([a29137b](https://github.com/onion-4-dinner/yellowjacket/commit/a29137b2ba4c6b33ce9a5f868cbd6013e0e3b116))
* include full track metadata in GetAudioFilesByReleaseGroup query ([97f256d](https://github.com/onion-4-dinner/yellowjacket/commit/97f256d67f463d752f7adc5b400c4bf34eae1df1))
* **quick-10:** add migration 5 and fix entity cache for composite album key ([d43ba7b](https://github.com/onion-4-dinner/yellowjacket/commit/d43ba7bd0c7ace2a9ed71990a19498f8e9f90751))
* **quick-10:** update release_groups schema and queries for composite uniqueness ([999ab96](https://github.com/onion-4-dinner/yellowjacket/commit/999ab967beb9107a3f30ba287acbffad22f0b0de))
* **quick-13:** resolve lint issues in main source files ([e1a95e6](https://github.com/onion-4-dinner/yellowjacket/commit/e1a95e65a9f0f436b2e2d92befa9c881b6e8e430))
* **quick-14:** add roll-back-on-failure to queue index advancement ([2820de2](https://github.com/onion-4-dinner/yellowjacket/commit/2820de2510560fcd6d1015c18542d5ac30468247))
* **quick-9:** set fixed height on queue track items for stable virtualizer scroll ([ebde5e5](https://github.com/onion-4-dinner/yellowjacket/commit/ebde5e5a8bc4da8f40bef8f171c7ed86c213a336))

### Performance

* **07-01:** add incremental persistence helpers for queue mutations ([cdd17db](https://github.com/onion-4-dinner/yellowjacket/commit/cdd17db27509908514c21517631306655a2b3bd7))
* **07-01:** eliminate redundant lookups in SetQueue Phase 2 ([ced58fe](https://github.com/onion-4-dinner/yellowjacket/commit/ced58fe6a93d6f220137562b8ff09ffc33c69266))
* **07-02:** defer eagerFetch to after DOM ready for instant app shell ([cd98ad6](https://github.com/onion-4-dinner/yellowjacket/commit/cd98ad6dc8c2e4e6e0f01a48099b0c0511bf5a98))
* **08-01:** add queueMicrotask coalescing to library store and debounce search input ([3bf66ed](https://github.com/onion-4-dinner/yellowjacket/commit/3bf66ed125ed55bfbde95b0bc973710c2f2243b8))
* **08-02:** migrate cover-grid, artists-view, and genres-view virtualizers to repeat() directive ([1c3514d](https://github.com/onion-4-dinner/yellowjacket/commit/1c3514da1d0491b9758d7a6f9f72d59ef78fc8ed))
* **08-02:** migrate track-list and queue-panel virtualizers to repeat() directive ([d2d7d8c](https://github.com/onion-4-dinner/yellowjacket/commit/d2d7d8c6ce22923772cae4858b02804d15f74bb7))
* **08-03:** optimize column rendering and apply classMap to queue-panel renderTrackItem ([62f41c2](https://github.com/onion-4-dinner/yellowjacket/commit/62f41c24910632b270f9f5765e20e48db4b95ec9))
* **08-03:** replace class string construction with classMap directive in renderTrackRow ([ad21027](https://github.com/onion-4-dinner/yellowjacket/commit/ad210278fc20729dc76390e6bba9bff050549046))

### Refactoring

* **06-01:** consolidate search queries to use track_metadata VIEW ([9159b40](https://github.com/onion-4-dinner/yellowjacket/commit/9159b409dcd2afaa7dcc97bf5b0694edf85f06a4))
* **quick-14:** make playOrLoadCurrentTrack and playCurrentTrack return bool ([6eeddda](https://github.com/onion-4-dinner/yellowjacket/commit/6eeddda97669258cc5b7ba175a3c98d598a2871f))

## [1.1.3](https://github.com/onion-4-dinner/yellowjacket/compare/v1.1.2...v1.1.3) (2026-02-21)

### Bug Fixes

* add typescript as explicit devDependency and auto-install frontend deps in setup ([#70](https://github.com/onion-4-dinner/yellowjacket/issues/70)) ([7316587](https://github.com/onion-4-dinner/yellowjacket/commit/73165877fa79656ab9bc6f60bd8e9e52d6be206c))
* use local tsc binary in pre-commit hook to avoid PATH issues ([#71](https://github.com/onion-4-dinner/yellowjacket/issues/71)) ([6079e55](https://github.com/onion-4-dinner/yellowjacket/commit/6079e558ff913d38c7f1c4aeb52cc09474c4ed20))

## [1.1.2](https://github.com/onion-4-dinner/yellowjacket/compare/v1.1.1...v1.1.2) (2026-02-15)

### Bug Fixes

* r2 upload ([#69](https://github.com/onion-4-dinner/yellowjacket/issues/69)) ([0252466](https://github.com/onion-4-dinner/yellowjacket/commit/0252466f615b4e2fd9694790c6d311a9eac1ccf2))

## [1.1.1](https://github.com/onion-4-dinner/yellowjacket/compare/v1.1.0...v1.1.1) (2026-02-15)

### Bug Fixes

* **ci:** remove build-check job from CI workflow ([#66](https://github.com/onion-4-dinner/yellowjacket/issues/66)) ([42d3f45](https://github.com/onion-4-dinner/yellowjacket/commit/42d3f45d85afa694e9545997af3ff4ac814ad021))

## [1.1.0](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.3...v1.1.0) (2026-02-15)

### Features

* **ci:** upload release artifacts to Cloudflare R2 ([#65](https://github.com/onion-4-dinner/yellowjacket/issues/65)) ([8985084](https://github.com/onion-4-dinner/yellowjacket/commit/89850848cbf7783e5c85348ff18f7cd11d60231a))

## [1.0.3](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.2...v1.0.3) (2026-02-15)

### ⚠ BREAKING CHANGES

* **deps:** update module github.com/evilmartians/lefthook to v2 (#61)
* **deps:** update actions/checkout action to v6 (#45)
* **deps:** update dependency vite to v7 (#53)

### Bug Fixes

* resolve all lint errors and make linting a required CI check ([#62](https://github.com/onion-4-dinner/yellowjacket/issues/62)) ([30b2480](https://github.com/onion-4-dinner/yellowjacket/commit/30b2480df49f57878b0e8c923da6ad8d6fe99416))
* virtual list and cover grid ([#63](https://github.com/onion-4-dinner/yellowjacket/issues/63)) ([7579a76](https://github.com/onion-4-dinner/yellowjacket/commit/7579a768be84225ed46db4e7a90781f3e30e2953))

### Miscellaneous

* **deps:** update actions/checkout action to v6 ([#45](https://github.com/onion-4-dinner/yellowjacket/issues/45)) ([2d6e221](https://github.com/onion-4-dinner/yellowjacket/commit/2d6e22105d2daed1dc5b586c0442e2941949a165))
* **deps:** update dependency vite to v7 ([#53](https://github.com/onion-4-dinner/yellowjacket/issues/53)) ([f0006c4](https://github.com/onion-4-dinner/yellowjacket/commit/f0006c4c4335b60b58cccdd29de4792965e39694))
* **deps:** update module github.com/evilmartians/lefthook to v2 ([#61](https://github.com/onion-4-dinner/yellowjacket/issues/61)) ([e32b217](https://github.com/onion-4-dinner/yellowjacket/commit/e32b2179129ae7f26037697a125710ff7587566d))

## [1.0.2](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.1...v1.0.2) (2026-02-14)

### ⚠ BREAKING CHANGES

* **deps:** update actions/setup-node action to v6 (#48)
* **deps:** update dependency stylelint-config-standard to v40 (#52)
* **deps:** update dependency node to v24 (#51)
* **deps:** update dependency vite-plugin-static-copy to v3 (#54)
* **deps:** update golangci/golangci-lint-action action to v9 (#55)
* **deps:** update amannn/action-semantic-pull-request action to v6 (#50)
* **deps:** update actions/upload-artifact action to v6 (#49)
* **deps:** update actions/setup-go action to v6 (#47)
* **deps:** update actions/download-artifact action to v7 (#46)

### Bug Fixes

* **ci:** use allowedPostUpgradeCommands for Renovate post-upgrade tasks ([#60](https://github.com/onion-4-dinner/yellowjacket/issues/60)) ([0aef483](https://github.com/onion-4-dinner/yellowjacket/commit/0aef483b3cccd0616fd5be2d06d0856b46851d09))

### Miscellaneous

* **deps:** update actions/download-artifact action to v7 ([#46](https://github.com/onion-4-dinner/yellowjacket/issues/46)) ([1910f99](https://github.com/onion-4-dinner/yellowjacket/commit/1910f99cf64e9bdc5ce91e89cab254ecca15d030))
* **deps:** update actions/setup-go action to v6 ([#47](https://github.com/onion-4-dinner/yellowjacket/issues/47)) ([8911fb2](https://github.com/onion-4-dinner/yellowjacket/commit/8911fb2400047cf2f3dfa719edc1d1bf474cdaa5))
* **deps:** update actions/setup-node action to v6 ([#48](https://github.com/onion-4-dinner/yellowjacket/issues/48)) ([d7382fd](https://github.com/onion-4-dinner/yellowjacket/commit/d7382fd8444b6618dbfe991f5f97231528a07f13))
* **deps:** update actions/upload-artifact action to v6 ([#49](https://github.com/onion-4-dinner/yellowjacket/issues/49)) ([a2c644b](https://github.com/onion-4-dinner/yellowjacket/commit/a2c644b00eed83acc0ed38a2eb8c73868b7b79af))
* **deps:** update amannn/action-semantic-pull-request action to v6 ([#50](https://github.com/onion-4-dinner/yellowjacket/issues/50)) ([643ba27](https://github.com/onion-4-dinner/yellowjacket/commit/643ba27f066164aeb47e8d9aaf20fe98b9b69d30))
* **deps:** update dependency node to v24 ([#51](https://github.com/onion-4-dinner/yellowjacket/issues/51)) ([e7d3971](https://github.com/onion-4-dinner/yellowjacket/commit/e7d39711078ce86b0c029f0d03ff81162c5dc28a))
* **deps:** update dependency stylelint-config-standard to v40 ([#52](https://github.com/onion-4-dinner/yellowjacket/issues/52)) ([422aabc](https://github.com/onion-4-dinner/yellowjacket/commit/422aabcc07e9700ff189302b363e13d87c69163a))
* **deps:** update dependency vite-plugin-static-copy to v3 ([#54](https://github.com/onion-4-dinner/yellowjacket/issues/54)) ([77fa643](https://github.com/onion-4-dinner/yellowjacket/commit/77fa6435a5298f58ef83607d99c59b876132c66c))
* **deps:** update golangci/golangci-lint-action action to v9 ([#55](https://github.com/onion-4-dinner/yellowjacket/issues/55)) ([aedb7d1](https://github.com/onion-4-dinner/yellowjacket/commit/aedb7d1e6d204c56c468dd26b340752fd6bfeaeb))

## [1.0.1](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.0...v1.0.1) (2026-02-14)

### Bug Fixes

* resolve Renovate repo detection and pre-push hook hang ([#36](https://github.com/onion-4-dinner/yellowjacket/issues/36)) ([b205889](https://github.com/onion-4-dinner/yellowjacket/commit/b205889128f01e9eb75b607cf7c4034887cda3f4))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* allow library to initialize without config and fix lefthook lint flag ([5a958db](https://github.com/onion-4-dinner/yellowjacket/commit/5a958db16284a74e19c43259757b163b347cda7d))
* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** fix golangci-lint version, skip player test in CI, remove standalone frontend build ([7317e09](https://github.com/onion-4-dinner/yellowjacket/commit/7317e093a7f92651ab65b2f83381d02105bdc0df))
* **ci:** resolve CI failures for Go checks, codegen, and frontend type-checking ([d4f9361](https://github.com/onion-4-dinner/yellowjacket/commit/d4f936143ac75fbf3247cdbe2113bd89b0795d83))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))
* rename downloaded artifacts to platform-specific names for release ([e3bda0e](https://github.com/onion-4-dinner/yellowjacket/commit/e3bda0e2fc7700fad382cabe00aeb46f91fbb0a0))
* resolve frontend build failures in CI ([330a53c](https://github.com/onion-4-dinner/yellowjacket/commit/330a53c9f4b1292840ad0f75479b76b3d429c954))
* trigger build workflow from release event instead of tag push ([47772f7](https://github.com/onion-4-dinner/yellowjacket/commit/47772f73cc04093c55414bf20ebe2ef442418d19))
* use path.Join for embed.FS paths to fix Windows build ([672fe24](https://github.com/onion-4-dinner/yellowjacket/commit/672fe24ee99debf4a394fff7eec55f17b0e44476))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* allow library to initialize without config and fix lefthook lint flag ([5a958db](https://github.com/onion-4-dinner/yellowjacket/commit/5a958db16284a74e19c43259757b163b347cda7d))
* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** fix golangci-lint version, skip player test in CI, remove standalone frontend build ([7317e09](https://github.com/onion-4-dinner/yellowjacket/commit/7317e093a7f92651ab65b2f83381d02105bdc0df))
* **ci:** resolve CI failures for Go checks, codegen, and frontend type-checking ([d4f9361](https://github.com/onion-4-dinner/yellowjacket/commit/d4f936143ac75fbf3247cdbe2113bd89b0795d83))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))
* resolve frontend build failures in CI ([330a53c](https://github.com/onion-4-dinner/yellowjacket/commit/330a53c9f4b1292840ad0f75479b76b3d429c954))
* trigger build workflow from release event instead of tag push ([47772f7](https://github.com/onion-4-dinner/yellowjacket/commit/47772f73cc04093c55414bf20ebe2ef442418d19))
* use path.Join for embed.FS paths to fix Windows build ([672fe24](https://github.com/onion-4-dinner/yellowjacket/commit/672fe24ee99debf4a394fff7eec55f17b0e44476))

## [1.0.3](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.2...v1.0.3) (2026-02-14)

### Bug Fixes

* use path.Join for embed.FS paths to fix Windows build ([672fe24](https://github.com/onion-4-dinner/yellowjacket/commit/672fe24ee99debf4a394fff7eec55f17b0e44476))

## [1.0.2](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.1...v1.0.2) (2026-02-14)

### Bug Fixes

* resolve frontend build failures in CI ([330a53c](https://github.com/onion-4-dinner/yellowjacket/commit/330a53c9f4b1292840ad0f75479b76b3d429c954))

## [1.0.1](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.0...v1.0.1) (2026-02-14)

### Bug Fixes

* allow library to initialize without config and fix lefthook lint flag ([5a958db](https://github.com/onion-4-dinner/yellowjacket/commit/5a958db16284a74e19c43259757b163b347cda7d))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** fix golangci-lint version, skip player test in CI, remove standalone frontend build ([7317e09](https://github.com/onion-4-dinner/yellowjacket/commit/7317e093a7f92651ab65b2f83381d02105bdc0df))
* **ci:** resolve CI failures for Go checks, codegen, and frontend type-checking ([d4f9361](https://github.com/onion-4-dinner/yellowjacket/commit/d4f936143ac75fbf3247cdbe2113bd89b0795d83))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))

## [1.0.2](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.1...v1.0.2) (2026-02-14)

### Bug Fixes

* **ci:** fix golangci-lint version, skip player test in CI, remove standalone frontend build ([7317e09](https://github.com/onion-4-dinner/yellowjacket/commit/7317e093a7f92651ab65b2f83381d02105bdc0df))

## [1.0.1](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.0...v1.0.1) (2026-02-14)

### Bug Fixes

* **ci:** resolve CI failures for Go checks, codegen, and frontend type-checking ([d4f9361](https://github.com/onion-4-dinner/yellowjacket/commit/d4f936143ac75fbf3247cdbe2113bd89b0795d83))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))
