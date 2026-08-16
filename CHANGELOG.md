# Changelog

## [0.12.0](https://github.com/agjmills/trove/compare/v0.11.0...v0.12.0) (2026-08-16)


### Features

* add /tmp volume mount for file uploads in Docker setup ([de17022](https://github.com/agjmills/trove/commit/de1702225d1e948fb5f5749e984aec18a00e3022))
* Add admin dashboard with user management features ([ca4dc6e](https://github.com/agjmills/trove/commit/ca4dc6e79bd00d3471f6c2a9adbb1113e258e050))
* Add aria-labels for accessibility on restore and delete buttons in deleted items ([7aeb3e9](https://github.com/agjmills/trove/commit/7aeb3e910ccef1a967b4a227e13991a6e025cb53))
* Add autocomplete attributes to password fields for improved user experience ([420e2bc](https://github.com/agjmills/trove/commit/420e2bc5d81e638e1dc5f90f8ca3ea236c672233))
* add cancellation callback to chunked upload manager ([3c566b1](https://github.com/agjmills/trove/commit/3c566b11f0d7548cb9b3cacf6353633b70a131d8))
* add chunk contiguity verification in CompleteUpload handler ([f4cadbd](https://github.com/agjmills/trove/commit/f4cadbdf90abd89c94d9ad370dd2f500df19096c))
* Add CI workflow for testing and building Go application ([9a6d16c](https://github.com/agjmills/trove/commit/9a6d16cb1279f647dae5ecdda716cb894236b700))
* Add comprehensive integration tests for handlers and routes ([49cb4e0](https://github.com/agjmills/trove/commit/49cb4e0a6acd51d5638339e2efd93f6217f0b890))
* Add confirmation prompts for folder and file deletion actions ([0832187](https://github.com/agjmills/trove/commit/0832187aa416fd69e82b4b85c2de8d9f30259529))
* Add create folder modal and enhance sidebar functionality ([370539c](https://github.com/agjmills/trove/commit/370539cb39327ba4e8dba5300e3e1f680bc00166))
* Add CSRF token to registration error rendering for enhanced security ([710a34f](https://github.com/agjmills/trove/commit/710a34fee88b9b720f10ecaf542949dac90840cc))
* Add file preview and view page with metadata and actions ([758c0f4](https://github.com/agjmills/trove/commit/758c0f423270290576b2b56d1b488c75ce4cdd4f))
* add file sorting by filename, size, created_at with URL persist… ([#112](https://github.com/agjmills/trove/issues/112)) ([9d86764](https://github.com/agjmills/trove/commit/9d867648a37762c2381c6e157db8d0dacf1e351b))
* Add filename and folder name length validation in rename functions ([52c40c9](https://github.com/agjmills/trove/commit/52c40c908a17017901f088b45cb615ce31789c12))
* Add FullWidth flag to ChangePassword response for consistent UI rendering ([0152d13](https://github.com/agjmills/trove/commit/0152d132b490836cb4f5e0c27c674f7f212ad9db))
* add immediate cleanup of expired upload sessions on startup ([223d8d2](https://github.com/agjmills/trove/commit/223d8d2cf5ebafd08126ab9771ed4d8883fa2ee9))
* Add integration tests for admin routes and user management functionality ([151d7bc](https://github.com/agjmills/trove/commit/151d7bc428daccf513b1cd2ef9c152ea2f5859a3))
* add logical path normalization to prevent directory traversal attacks ([d9db5bd](https://github.com/agjmills/trove/commit/d9db5bdbd756141d5efd31de6abe731dd9163ad9))
* add mutex for concurrent chunk updates and improve file hash calculation ([06f1877](https://github.com/agjmills/trove/commit/06f1877c59b680a33329affeb23c512360b38776))
* add OIDC/SSO authentication ([#70](https://github.com/agjmills/trove/issues/70)) ([0fc4b96](https://github.com/agjmills/trove/commit/0fc4b963f3773e49b3532a6994172ccbfe38b159))
* Add rate limiting and CSRF protection to change password endpoint ([c8e7b42](https://github.com/agjmills/trove/commit/c8e7b42e18319f616d4796e51312e7bceaf7a260))
* Add security documentation and update CSRF protection details in README and SECURITY.md ([15d2bad](https://github.com/agjmills/trove/commit/15d2bad0569813d5d630c60fffd836f001e387c7))
* Add storage percentage calculation and enhance UI elements across templates ([756b93a](https://github.com/agjmills/trove/commit/756b93ab5cab87cba371468567ec25bb2a745092))
* add tests for upload status retrieval and completion scenarios ([5c19dfa](https://github.com/agjmills/trove/commit/5c19dfa07549d5934b81c9b36144016e2d61560d))
* add toast container existence check in showToast function ([482a165](https://github.com/agjmills/trove/commit/482a165a21be48e2c11fd2ca115bc1a297d1ab42))
* Add unauthorized user check in ShowDeleted handler ([262e48b](https://github.com/agjmills/trove/commit/262e48b06d73308a4fa3400edcc6e99a4ddc926f))
* add upload session retention configuration and improve session cleanup logic ([4c98fef](https://github.com/agjmills/trove/commit/4c98fef660844ee26121741ff5fa51a8382631b6))
* add upload session retention days to configuration and clean up test code ([7e9d747](https://github.com/agjmills/trove/commit/7e9d747fecb555a72be3e3bf74eb5fd65b5cd8d3))
* add upload speed and ETA display during file upload ([fdd8039](https://github.com/agjmills/trove/commit/fdd803914ec2769944d536e165a349e65370834a))
* Add validation for existing source folder in renameFolder function ([0d8395f](https://github.com/agjmills/trove/commit/0d8395f49e3cf7f646011c5126151366fd491a89))
* Add validation for new password length in ChangePassword handler ([29f6f48](https://github.com/agjmills/trove/commit/29f6f483fc6d5bf6b6b26cca13aa4c86e75f5b62))
* Add visual separators in delete forms for better UI clarity ([6097b49](https://github.com/agjmills/trove/commit/6097b49cbe329e83098ad17f4336d8da41e303fb))
* adjust retention period logic to default to 7 days for negative values in CleanupExpiredSessions ([019f16a](https://github.com/agjmills/trove/commit/019f16a2aa879ee766d3cd215293bd79c15fe287))
* clean up whitespace in upload session model and handler ([ec177b1](https://github.com/agjmills/trove/commit/ec177b1d7ab37444a55e7bf1644c8301c1937213))
* Enhance admin authorization checks in AdminEmptyAllDeleted handler ([4b3a9d5](https://github.com/agjmills/trove/commit/4b3a9d5f94fdee7bd8a1dd83d350995000da3c1d))
* enhance cancelUpload method to handle missing session gracefully ([c63a04e](https://github.com/agjmills/trove/commit/c63a04e169b44b20503ae731c1b2daa4b1ff8c7e))
* enhance CancelUpload to log upload ID on failure and perform async cleanup ([7afd531](https://github.com/agjmills/trove/commit/7afd531ade48a834dc6bcc474c495dd694d75dcb))
* enhance environment configuration and CSRF protection handling ([8fad730](https://github.com/agjmills/trove/commit/8fad7306317ff5d8921d2c72f3cee0e7a9418626))
* enhance error handling in CompleteUpload by cleaning up temp directory ([74946e9](https://github.com/agjmills/trove/commit/74946e9c0cacb879e3e871ad839e33d7d3e6d271))
* Enhance error handling in PermanentlyDeleteFolder and update test for unauthorized access ([4e84b36](https://github.com/agjmills/trove/commit/4e84b3639c34083c9f0296e81dc404e9a469da35))
* Enhance file menu with download, rename, and move options ([8c14bd8](https://github.com/agjmills/trove/commit/8c14bd87dfff2ea8899b61ba14bae892910b7f6e))
* enhance folder handling with natural sorting and unique IDs for improved UI ([cf84117](https://github.com/agjmills/trove/commit/cf84117edba676f587cc7a5ae45cd728a28817e1))
* Enhance folder moving functionality with improved checks and logging ([44c14ac](https://github.com/agjmills/trove/commit/44c14ac0ad8c8e4e7c34704fe5e6335106625a22))
* Enhance getClientIP function to support X-Forwarded-For header for multi-hop proxies ([ca86f7d](https://github.com/agjmills/trove/commit/ca86f7d4153c7a8e80908c33e0c65d64ab7c9f7e))
* Enhance mobile menu positioning and accessibility with dynamic updates ([8d2ff4d](https://github.com/agjmills/trove/commit/8d2ff4d8f7cc43d19f2358a0ac3b6044d66db741))
* Enhance mobile menu with login and registration options for unauthenticated users ([92f2e52](https://github.com/agjmills/trove/commit/92f2e52735a237d31b7150d2c14ea28f9c573089))
* Enhance modal accessibility and improve event delegation for user management ([63a09b6](https://github.com/agjmills/trove/commit/63a09b69584b4e6d5ce2e0241f05751e4e6ce0c8))
* enhance README with updated UI and Docker deployment details ([07eb8b6](https://github.com/agjmills/trove/commit/07eb8b60bc26f3d60121db5cbe51ff50b6dbae60))
* Enhance security headers for preview routes with appropriate CSP and X-Frame-Options ([d4c3d27](https://github.com/agjmills/trove/commit/d4c3d27633f922a6b1d626eae22618dc27247836))
* enhance toast notification rendering with improved DOM manipulation and XSS prevention ([10fb33b](https://github.com/agjmills/trove/commit/10fb33b5157169e73ada436d4505903ad0675327))
* Enhance upload functionality with improved drag overlay management and upload speed tracking ([a798181](https://github.com/agjmills/trove/commit/a7981818d1fc2bd74e5ab94d636a9ce4f7941af1))
* Enhance upload state visibility and error handling ([78439ba](https://github.com/agjmills/trove/commit/78439baf8b6dd11e676b4320ee947486e551f4b1))
* enhance version extraction in release workflow for Docker tagging ([f9ac75e](https://github.com/agjmills/trove/commit/f9ac75eee29c0f4de50f470e06a356679bf69e9b))
* ensure upload session cleanup after completion, even on error ([bcb70a2](https://github.com/agjmills/trove/commit/bcb70a27d5dd773242bbfa94f1756d51fe12fb70))
* file sharing links ([#77](https://github.com/agjmills/trove/issues/77)) ([a53bdd5](https://github.com/agjmills/trove/commit/a53bdd5255518a4cf9efa135939831e403d72840))
* handle error when marking upload session as expired ([eede038](https://github.com/agjmills/trove/commit/eede038a595565fb47da158b9f19bc4ba2ef6c84))
* **handlers:** add escapeSQLLike function and corresponding tests ([0aa36a3](https://github.com/agjmills/trove/commit/0aa36a30327b01a98c6954a58778a4abc4a7c36e))
* Implement admin user management features ([018609e](https://github.com/agjmills/trove/commit/018609e382b33e890a55a33829074bcc86903c66))
* implement chunked file upload handler with resumable uploads ([c682350](https://github.com/agjmills/trove/commit/c682350448c31d2063abeae5b24913f2ea5137ad))
* Implement file and folder renaming functionality with UI support ([de32a85](https://github.com/agjmills/trove/commit/de32a858a3313cdfacde9ab45ddb4f210e18eb1b))
* implement flash message handling with dedicated partial template for toast notifications ([e12a79b](https://github.com/agjmills/trove/commit/e12a79b6769fee016694b2eee976ffe481cbaaf7))
* Implement focus trap utility for improved modal accessibility ([38e3f7c](https://github.com/agjmills/trove/commit/38e3f7cb79ad6382da48ab0cd20d755cc64090cd))
* Implement folder moving functionality with UI support ([69802a3](https://github.com/agjmills/trove/commit/69802a37c8bd35ed5b6412d2b846e8dbba14442f))
* implement graceful shutdown for upload session cleanup worker ([9cf885d](https://github.com/agjmills/trove/commit/9cf885d5aff0152d56e0e2ce0c32f846ae76ae6e))
* implement natural sorting for files and folders with pagination support ([c55d2cb](https://github.com/agjmills/trove/commit/c55d2cbf2a4c03755c241f8783c14e6b99bca635))
* Implement plaintextCSRF middleware for enhanced CSRF protection based on environment ([f1c9237](https://github.com/agjmills/trove/commit/f1c923755b759ddaee3b172066fe3f99648b8181))
* Implement registration feature toggle and update UI for login and registration pages ([e5915fe](https://github.com/agjmills/trove/commit/e5915fe1044a947b36e45e72e01dd0e20fa42d98))
* implement transaction for atomic file record creation and storage update in CompleteUpload ([9aa1fdb](https://github.com/agjmills/trove/commit/9aa1fdb128152ee8a0fc0e703b7fb0fcaf092228))
* Implement trusted proxy configuration for enhanced CSRF protection and validation ([f0cd4b8](https://github.com/agjmills/trove/commit/f0cd4b8317a799f5897f3cff26282fde08f0c809))
* Improve admin handler tests with dynamic user ID handling and error checks ([8488255](https://github.com/agjmills/trove/commit/8488255502c36ea8a162f319aced50d32cca82bb))
* improve breadcrumb navigation and remove parent directory link ([6cdfa42](https://github.com/agjmills/trove/commit/6cdfa42aa6aa5f612241cfec1a3f78b27e3280ff))
* Improve deleted items cleanup efficiency and error handling ([1d72450](https://github.com/agjmills/trove/commit/1d724503e1da7c4de03161dfda5c0e7ece2b20e3))
* improve error handling in upload process by catching cancellation errors ([c2632ce](https://github.com/agjmills/trove/commit/c2632ce57c99435c035fcea2ff0918ac6c0c7e82))
* improve path normalization and enhance temp directory handling in uploads ([69bead0](https://github.com/agjmills/trove/commit/69bead0ce32bc0ec5ed787d3bb54e73412f21f0d))
* Improve success message for permanently deleting folders with fallback for empty names ([6002848](https://github.com/agjmills/trove/commit/600284862b62ec5d64573ed70325287c5bbe3206))
* Improve user registration to prevent race conditions for admin assignment ([42fdddd](https://github.com/agjmills/trove/commit/42fddddd1322e0e44d15ba7ef22408c424d5fa6e))
* Migrate from gorilla/csrf to filippo.io/csrf for improved CSRF … ([66bff7c](https://github.com/agjmills/trove/commit/66bff7c2908335f8a882fe234d1e2d1ecc089470))
* Migrate from gorilla/csrf to filippo.io/csrf for improved CSRF protection ([ed6ac20](https://github.com/agjmills/trove/commit/ed6ac203f046d4844783363f73d0bbc3a6277235))
* **migrations:** implement storage_path migration from old schema ([e8824af](https://github.com/agjmills/trove/commit/e8824af584e731e09df60d7027c940870232c867))
* Optimize deleted items count query to combine file and folder counts ([66e90b0](https://github.com/agjmills/trove/commit/66e90b02c70f62efe039d10ba8141596df3d47f9))
* optimize file hashing by skipping client-side hash for large files ([1f58f88](https://github.com/agjmills/trove/commit/1f58f88ee0bedc098e6f6daaba1efc9cb4e0838c))
* Optimize folder retrieval for move dropdown using UNION query ([f65287c](https://github.com/agjmills/trove/commit/f65287ca0fd9b768115714ad441601bfb85c6ee4))
* reduce default page size from 50 to 15 for improved pagination ([2a1a12c](https://github.com/agjmills/trove/commit/2a1a12cf56cf88ba76695efb88dee82d40ee24bb))
* Refactor admin handler to remove session manager and improve error handling ([d73375a](https://github.com/agjmills/trove/commit/d73375ac49c86f12c433de115182bfde008f37e7))
* Refactor folder redirection logic into a dedicated function for improved maintainability ([809cc92](https://github.com/agjmills/trove/commit/809cc92b2d77353c99cadb3bcaf4f0d71bd3c27e))
* Refactor JSON request handling in authentication methods for improved readability and consistency ([c7e42e3](https://github.com/agjmills/trove/commit/c7e42e353bac739d722b16feb0bc18e79780c9fb))
* Refactor RequireAdmin middleware to remove unnecessary dependencies ([bf7599d](https://github.com/agjmills/trove/commit/bf7599d3b801039caf41e4dfcc88220b564edf2f))
* Refactor template helpers into a dedicated package for improved organization and reusability ([85cc054](https://github.com/agjmills/trove/commit/85cc0541e567921c4f3962ad4aa240612ac5dceb))
* Refactor upload form and progress display for improved clarity and structure ([2de03ee](https://github.com/agjmills/trove/commit/2de03eedfeb66889de9dda7e9df70fc19dc7bb2b))
* Refactor validation logic for deleted items configuration ([2207568](https://github.com/agjmills/trove/commit/2207568e07feaa61b45c994e324e72a60f61c856))
* Remove CSRF token handling and update security documentation for new protection model ([2fa1d6e](https://github.com/agjmills/trove/commit/2fa1d6e3f590c35b920004eecaf428ec02823d3c))
* Rename destination select field to improve clarity in move form ([1f1cd63](https://github.com/agjmills/trove/commit/1f1cd632db573efbfec4ac3724e56dd8d825d9df))
* Rename test users for clarity in deleted and download integration tests ([e58829e](https://github.com/agjmills/trove/commit/e58829efa5dcad1820be9f696128bf70247eca4b))
* Replace log statements with structured logger for deleted items cleanup ([c7ee396](https://github.com/agjmills/trove/commit/c7ee3960fea91ca623883ec1b5ba04fee0a45f08))
* Replace upload icon with SVG for improved visual clarity in drag-and-drop overlay ([9918260](https://github.com/agjmills/trove/commit/9918260615cca358e6af088a48262ff7f6f7075d))
* **search:** file search with tag support ([#90](https://github.com/agjmills/trove/issues/90)) ([a8c7041](https://github.com/agjmills/trove/commit/a8c70418e80a39202cdda7a5e3a72a43f0e7b96e))
* **shares:** folder sharing with optional password protection ([#88](https://github.com/agjmills/trove/issues/88)) ([28bc962](https://github.com/agjmills/trove/commit/28bc962f520f18de6e6bfc8e92f6a31efaae10f1))
* **shares:** password-protected share links ([#86](https://github.com/agjmills/trove/issues/86)) ([9d3d618](https://github.com/agjmills/trove/commit/9d3d618484487c60aeac3c2c95c005bd969cd7bd))
* show video conversion status in file list and file view ([145e4a0](https://github.com/agjmills/trove/commit/145e4a06cfc2d105de1bedf090ced482d22143a2))
* **site:** scaffold Hugo/Hextra GitHub Pages site ([#106](https://github.com/agjmills/trove/issues/106)) ([5861755](https://github.com/agjmills/trove/commit/5861755ae9863db76c501d7a9cd17aff33ab7ff3))
* Skip immediate cleanup in test environment to avoid race conditions ([12b40ca](https://github.com/agjmills/trove/commit/12b40ca118fc9ea49f739b8084c142928c5ba66b))
* Sort unique folder paths in ViewFile method for improved consistency ([6ecccd8](https://github.com/agjmills/trove/commit/6ecccd85b5835b5063e15d735c51883f104cd7bc))
* **storage:** add multi-backend storage abstraction with S3 support ([e6ee7a5](https://github.com/agjmills/trove/commit/e6ee7a536d8400db81b619ebd07b600f77700a64))
* **storage:** add multi-backend storage abstraction with S3 support ([9ed75ab](https://github.com/agjmills/trove/commit/9ed75ab43de28d3f6e63334262edcca57bd9fb91))
* **storage:** implement streaming uploads with hashing for Memory and S3 backends ([e9fb1aa](https://github.com/agjmills/trove/commit/e9fb1aa21dee10dabb1f6589f84e2f77ce198f29))
* Update configuration setup and refine unauthenticated password change test ([bae5072](https://github.com/agjmills/trove/commit/bae50720369cf61c1604af7d006822166dc33b7a))
* Update Content-Security-Policy headers for enhanced security in file preview ([510f463](https://github.com/agjmills/trove/commit/510f463297205c6938cb727c54ff7b4e8834d6f2))
* Update delete handler to soft delete files and adjust redirect logic ([3524525](https://github.com/agjmills/trove/commit/3524525277d8cd9e25b55dd5644464c24a18e716))
* Update Dependabot configuration for Go modules with pull request limits and versioning strategy ([5fffcf0](https://github.com/agjmills/trove/commit/5fffcf0878aeee7195288bb023efc85d8958ab64))
* update documentation ([cf6e77d](https://github.com/agjmills/trove/commit/cf6e77d3c14126cf3c221ae11c9398bed98b12f7))
* Update drag-and-drop upload text for clarity and consistency ([5415556](https://github.com/agjmills/trove/commit/5415556fc0b1564649cae7edab87766f2e98eaa4))
* Update file management interface and add settings page ([94a2b8c](https://github.com/agjmills/trove/commit/94a2b8c9d9bf9d768d9e2a541417279b8aa46c6c))
* update pagination logic to apply only to files, ensuring all folders are always displayed, display folders as buttons ([4820217](https://github.com/agjmills/trove/commit/4820217adc18ff98bef2efa6dc79d020ae335a1c))
* Update PLAN.md to reflect integration tests for handlers and routes ([330e753](https://github.com/agjmills/trove/commit/330e753136f07329837da4fec59978f15a26ffb5))
* **upload:** implement streaming uploads for large files ([048c4ae](https://github.com/agjmills/trove/commit/048c4ae2cf2b3c9b7c595806b55a5ac66624d776))
* validate chunked upload configuration settings ([d4b2816](https://github.com/agjmills/trove/commit/d4b28167dae6537bc5198af9e0c8d9e763948e34))
* Validate deleted items configuration for retention days and cleanup interval ([a762fb0](https://github.com/agjmills/trove/commit/a762fb0489dddd579c9860660b6be33ef25d187d))
* video transcoding with background ffmpeg worker and streaming ([d8e1179](https://github.com/agjmills/trove/commit/d8e1179951904756649692a330c8be61a581d3e1))


### Bug Fixes

* 102 ([#108](https://github.com/agjmills/trove/issues/108)) ([b9b3403](https://github.com/agjmills/trove/commit/b9b3403f1915f069ab5f4de8e06b7a88fa793e96))
* add auto-refresh restart logic for pending uploads ([fafed73](https://github.com/agjmills/trove/commit/fafed73efa9af347b532519ef5228115f7002454))
* Add CORS support with configurable allowed origins for SSE endpoints ([eb79a41](https://github.com/agjmills/trove/commit/eb79a41986abf7d0afe35f061b29793f7f9bad0a))
* Add newline at end of file in naturalLess function ([1133bc8](https://github.com/agjmills/trove/commit/1133bc8fbbcec31acb4b49439f2059e7305554fb))
* Add pending jobs tracking and wait functionality for background uploads ([0b5604d](https://github.com/agjmills/trove/commit/0b5604d7974e1e3d7ca877e477bd6ffce51b501f))
* add unload warning for active uploads to prevent data loss ([601fc54](https://github.com/agjmills/trove/commit/601fc54ce22e185eb7fca89c1a50d49df3bd967c))
* **admin:** improve IDP change UX ([#85](https://github.com/agjmills/trove/issues/85)) ([23fb120](https://github.com/agjmills/trove/commit/23fb120c130e9dd002bbc3fb8662bdc926b53b50))
* build docker images separately from goreleaser ([#72](https://github.com/agjmills/trove/issues/72)) ([fc26e26](https://github.com/agjmills/trove/commit/fc26e26a007c9b6c77f2b6dc317bd79069f162ed))
* clarify client-side quota check is best-effort, relies on page reload ([f3b6f9d](https://github.com/agjmills/trove/commit/f3b6f9daf2fa7121a5e5105fbd06584f82c7c727))
* Clean up untracked entries in upload state to prevent memory growth ([b4caede](https://github.com/agjmills/trove/commit/b4caedee30163f472827d1638f4599258a1a7693))
* Clean up whitespace and ensure newline at end of file in file_handler.go and file_handler_test.go ([251b0af](https://github.com/agjmills/trove/commit/251b0af479c2d1f0d131a03516b61634255696e4))
* correct docs nav link URL ([8b52ba3](https://github.com/agjmills/trove/commit/8b52ba384a9d9e73de3522ed8169de0dd9a2b88b))
* correct GORM query syntax in upload handler tests ([a1f938b](https://github.com/agjmills/trove/commit/a1f938b8c2f1ec1adf2e7fba4292950a06fc87a9))
* Correctly scope breadcrumb path handling in folder move logic ([062be1a](https://github.com/agjmills/trove/commit/062be1ae96bdd3a5ae25af7127ac29eba633bac5))
* Correctly scope breadcrumb path handling in folder move logic ([e933710](https://github.com/agjmills/trove/commit/e93371088b862c14a303804309f0fb8f64cd3589))
* Enhance cleanup of failed uploads to ensure idempotency and restore user storage quota ([0312485](https://github.com/agjmills/trove/commit/0312485604ad7b52e9870c3aab052be5c62c0205))
* Enhance error handling in CSRF protection and user creation tests ([ecf5f23](https://github.com/agjmills/trove/commit/ecf5f232a4746597919e14c18ec182d38f4fda85))
* Enhance error handling in integration tests for directory management and file creation ([e69f0be](https://github.com/agjmills/trove/commit/e69f0beaf69924e91b3c86e0e2536650aa05be90))
* Enhance responsive design for user management section in admin_users.html ([91191fb](https://github.com/agjmills/trove/commit/91191fb7674460eee65858efcfe176928a22b870))
* Enhance session handling in authenticated requests for integration tests ([7782a7f](https://github.com/agjmills/trove/commit/7782a7fec40283124b460d1585620cfaa40effaf))
* Enhance storage usage display with progress indication in files.html ([c71c1ed](https://github.com/agjmills/trove/commit/c71c1ed93dda919411a6fcbdee67ca30f429413b))
* Enhance upload state visibility and error handling in file management ([33227d6](https://github.com/agjmills/trove/commit/33227d637cde641926a4bbb24fbdd3e08cea9734))
* Ensure form submission only occurs if the form exists in delete user confirmation ([e1669c5](https://github.com/agjmills/trove/commit/e1669c5be8130e0aa3b4277d43fe70f55f43c568))
* go fmt ([114b09f](https://github.com/agjmills/trove/commit/114b09f1c4d3c32adec20d6e2068d5b5c4ec10ee))
* handle error when resetting file pointer in CompleteUpload ([dff4a44](https://github.com/agjmills/trove/commit/dff4a44f528b2640a538acf395897d87187758a6))
* **handlers:** improve file upload error handling and filename uniqueness ([8be5958](https://github.com/agjmills/trove/commit/8be5958df49e8b8f3e1fccd4182430283de46061))
* Implement adaptive polling for file status updates to improve performance ([6cb51a7](https://github.com/agjmills/trove/commit/6cb51a739159e25990137d9e22c6642e2e594bf2))
* Implement transactional cleanup of failed uploads to restore user storage quota ([dea1ea6](https://github.com/agjmills/trove/commit/dea1ea6030e8d688f1b52be998fdc9df1cfcb560))
* implement two-pass approach for multipart upload handling ([1e52167](https://github.com/agjmills/trove/commit/1e5216754a51fefc2bd2bee3b98bbcf479002383))
* implement two-pass approach for multipart upload handling ([c518255](https://github.com/agjmills/trove/commit/c518255cab3401066dbf4ad7e6faaebd612d2b6a))
* Improve dropdown menu positioning for small screens in files.html ([718afea](https://github.com/agjmills/trove/commit/718afeaedace112eeb9dbae24278342c515a7003))
* Improve error handling for JSON unmarshalling in integration tests ([f7ec67f](https://github.com/agjmills/trove/commit/f7ec67f642c6bd731226460e3a6c0229651fd656))
* Improve error handling in admin dashboard stats and quota update tests ([0fb7363](https://github.com/agjmills/trove/commit/0fb73635efd8ee8ec771e3de3f68e242fa9c5722))
* Improve handling of failed uploads and restore user storage quota ([c1b5ed6](https://github.com/agjmills/trove/commit/c1b5ed6d57d904e17942ec77a6ace158817d86f9))
* Improve handling of failed uploads and restore user storage quota ([ca062d3](https://github.com/agjmills/trove/commit/ca062d3e471282a7f83c4456bf905178c3f002d9))
* Improve page unload handling by cleaning up SSE connection and warning for active uploads ([755face](https://github.com/agjmills/trove/commit/755face9acd416a4d32b7251429fb1c92633cb5c))
* Improve responsive design for folder and file sections in files.html ([63b2693](https://github.com/agjmills/trove/commit/63b2693a57bab4862a12cc3682a8651c66ee4090))
* Improve visibility of failed file uploads in file listing tests ([0fd6256](https://github.com/agjmills/trove/commit/0fd6256e98eb77dc934e8cdd00ce63372f2110a7))
* inject version info into docker image via build args ([#75](https://github.com/agjmills/trove/issues/75)) ([d5d72b3](https://github.com/agjmills/trove/commit/d5d72b37562808a299d69b7000350fa9bd4e8638))
* **migrations:** enhance storage path migration for PostgreSQL and SQLite compatibility ([6c61a80](https://github.com/agjmills/trove/commit/6c61a80aacf4131396f5142ed84cd8e15cc6d911))
* **migrations:** enhance storage_path migration with transaction support and error handling ([e74fd47](https://github.com/agjmills/trove/commit/e74fd479104d8ccce6bcc74989d8b2c25e0c1f00))
* move auto-refresh timer declaration to prevent upload interruption ([5007d5c](https://github.com/agjmills/trove/commit/5007d5ccf78a6a81043cc5596d09778a4d25961f))
* **oidc:** don't trim trailing slash from issuer URL ([#79](https://github.com/agjmills/trove/issues/79)) ([dfee7ec](https://github.com/agjmills/trove/commit/dfee7ec3a9067e75b5480afbb183af36fb94df9c))
* Prevent page reload during active uploads to enhance user experience ([2af55b4](https://github.com/agjmills/trove/commit/2af55b4a13c9a79f2b381c73a30f82c8d595e48e))
* prevent upload interruption by canceling auto-refresh during file uploads ([e0f258a](https://github.com/agjmills/trove/commit/e0f258a8a4e6cd52ca021dedd393dcc40ece262f))
* **preview:** remove video preview ([#89](https://github.com/agjmills/trove/issues/89)) ([3ff8090](https://github.com/agjmills/trove/commit/3ff80908340cdfe9a14346ebf9bf98a4032f3eb2))
* Refactor hashing logic in S3 upload to handle retries correctly ([8540ab8](https://github.com/agjmills/trove/commit/8540ab89c37960a47a7c64b1989790b23b963249))
* Remove console logs from SSE connection handling for cleaner output ([72aa581](https://github.com/agjmills/trove/commit/72aa581b69cba1048183b348aa94a4795970472c))
* remove unnecessary CSRF header and update comment for upload endpoint ([71b32d5](https://github.com/agjmills/trove/commit/71b32d504f519718a549f93a650d71298bce15e0))
* Return error from cleanupFailedUpload and handle it in DismissFailedUpload ([7f186c0](https://github.com/agjmills/trove/commit/7f186c0b6b8fb2b6ee8e3efe8295ed6a3297e734))
* Sanitize error messages for failed uploads to enhance security and user experience ([96e38f8](https://github.com/agjmills/trove/commit/96e38f87a0e7ae7e0986f10aeb0d8032481905cf))
* **shares:** address coderabbit review feedback ([#80](https://github.com/agjmills/trove/issues/80)) ([2c8f86d](https://github.com/agjmills/trove/commit/2c8f86d17989dc237bd953a48bd6422fb24e8c8c))
* Simplify error message for failed storage uploads in file handler ([a88ac34](https://github.com/agjmills/trove/commit/a88ac34fbb33877da50f1e14cf6bb1e1c4a71958))
* Simplify folder path handling in move logic to improve readability ([ae6da1b](https://github.com/agjmills/trove/commit/ae6da1bf9b6820ba7d33ce2c709e352172075807))
* **storage:** ensure response body is drained in ValidateAccess for connection reuse ([ae1223c](https://github.com/agjmills/trove/commit/ae1223c7188aed5f5f44ac6662b10cbd00ceacc9))
* **storage:** improve concurrency handling in TestMemoryBackend_Save_MultipleConcurrent ([35f8693](https://github.com/agjmills/trove/commit/35f8693bf9ebc4f8457723b80f1ad5b4ef091b5e))
* **storage:** improve error handling in isNotExist function ([ae4825a](https://github.com/agjmills/trove/commit/ae4825abc9168d7dcf7b2e12ec16698483c60c97))
* **storage:** simplify error checking for S3 not found using strings.Contains ([c957496](https://github.com/agjmills/trove/commit/c9574962b5e20ef0bd0bb573340eb7a23520f14c))
* **tests:** improve error message validation in streaming error tests ([0cb442e](https://github.com/agjmills/trove/commit/0cb442eb531f71d14b85a42169fabfcba554da00))
* Update cmd/server/main.go ([e158228](https://github.com/agjmills/trove/commit/e1582285d12307d4f58f0afe2b13c43a6440506c))
* Update CSRF middleware comments for clarity and accuracy ([214b16c](https://github.com/agjmills/trove/commit/214b16c6cb032868c89aefe85299ae0bdb8e979d))
* Update database handling in integration tests for improved concurrency and error reporting ([4c1b108](https://github.com/agjmills/trove/commit/4c1b10843ae99d3779a84bc54bd0e79348e4498f))
* Update error message for upload failures to provide clearer guidance ([5e8ae70](https://github.com/agjmills/trove/commit/5e8ae70ca19cb930f19764cd0760934ac24ee096))
* Update indirect dependencies to latest versions for improved stability ([42ff0f9](https://github.com/agjmills/trove/commit/42ff0f9b51025c0e3d87cbf8c37e560a61a480a8))
* Update internal/handlers/file_handler.go ([f269674](https://github.com/agjmills/trove/commit/f26967474777bf3e97a84b656dd9132db828fe6a))
* Update internal/storage/storage.go ([e634a0c](https://github.com/agjmills/trove/commit/e634a0cc06c2915714126beb49fc711c5d43ff4c))
* use aws native sdk environment variables ([f75a577](https://github.com/agjmills/trove/commit/f75a5779295ec6c32fedaf2ab8d1183890dfc979))
* use GITHUB_TOKEN for release-please ([f145b81](https://github.com/agjmills/trove/commit/f145b8123f7ff739d4e0ec935e74e90a3952e223))
* use modernc pure-Go sqlite driver for CGO-free builds ([9f107b1](https://github.com/agjmills/trove/commit/9f107b12460a22201cc214aaa43f6bfd07572a34))
* use PAT for release-please PR creation ([f391721](https://github.com/agjmills/trove/commit/f391721235059a4982e32d56c15445cf3fec4b2c))

## [0.11.0](https://github.com/agjmills/trove/compare/v0.10.0...v0.11.0) (2026-08-16)


### Features

* show video conversion status in file list and file view ([145e4a0](https://github.com/agjmills/trove/commit/145e4a06cfc2d105de1bedf090ced482d22143a2))
* video transcoding with background ffmpeg worker and streaming ([d8e1179](https://github.com/agjmills/trove/commit/d8e1179951904756649692a330c8be61a581d3e1))


### Bug Fixes

* use GITHUB_TOKEN for release-please ([f145b81](https://github.com/agjmills/trove/commit/f145b8123f7ff739d4e0ec935e74e90a3952e223))
* use modernc pure-Go sqlite driver for CGO-free builds ([9f107b1](https://github.com/agjmills/trove/commit/9f107b12460a22201cc214aaa43f6bfd07572a34))
* use PAT for release-please PR creation ([f391721](https://github.com/agjmills/trove/commit/f391721235059a4982e32d56c15445cf3fec4b2c))

## [Unreleased]

### Features

* **video:** background transcoding to web-optimized H.264/AAC MP4 (max 720p, faststart) via a DB-backed job queue and separate ffmpeg worker container, with in-browser streaming and original download retention

## [0.10.0](https://github.com/agjmills/trove/compare/v0.9.0...v0.10.0) (2026-04-10)


### Features

* add file sorting by filename, size, created_at with URL persist… ([#112](https://github.com/agjmills/trove/issues/112)) ([9d86764](https://github.com/agjmills/trove/commit/9d867648a37762c2381c6e157db8d0dacf1e351b))
* **site:** scaffold Hugo/Hextra GitHub Pages site ([#106](https://github.com/agjmills/trove/issues/106)) ([5861755](https://github.com/agjmills/trove/commit/5861755ae9863db76c501d7a9cd17aff33ab7ff3))


### Bug Fixes

* natural sort order and renamed file repositioning ([#108](https://github.com/agjmills/trove/issues/108)) ([b9b3403](https://github.com/agjmills/trove/commit/b9b3403f1915f069ab5f4de8e06b7a88fa793e96))
* correct docs nav link URL ([8b52ba3](https://github.com/agjmills/trove/commit/8b52ba384a9d9e73de3522ed8169de0dd9a2b88b))

## [0.9.0](https://github.com/agjmills/trove/compare/v0.8.1...v0.9.0) (2026-04-07)


### Features

* **search:** file search with tag support ([#90](https://github.com/agjmills/trove/issues/90)) ([a8c7041](https://github.com/agjmills/trove/commit/a8c70418e80a39202cdda7a5e3a72a43f0e7b96e))
* **shares:** folder sharing with optional password protection ([#88](https://github.com/agjmills/trove/issues/88)) ([28bc962](https://github.com/agjmills/trove/commit/28bc962f520f18de6e6bfc8e92f6a31efaae10f1))
* **shares:** password-protected share links ([#86](https://github.com/agjmills/trove/issues/86)) ([9d3d618](https://github.com/agjmills/trove/commit/9d3d618484487c60aeac3c2c95c005bd969cd7bd))


### Bug Fixes

* **admin:** improve IDP change UX ([#85](https://github.com/agjmills/trove/issues/85)) ([23fb120](https://github.com/agjmills/trove/commit/23fb120c130e9dd002bbc3fb8662bdc926b53b50))
* **preview:** remove video preview ([#89](https://github.com/agjmills/trove/issues/89)) ([3ff8090](https://github.com/agjmills/trove/commit/3ff80908340cdfe9a14346ebf9bf98a4032f3eb2))

## [0.8.1](https://github.com/agjmills/trove/compare/v0.8.0...v0.8.1) (2026-04-05)


### Bug Fixes

* **oidc:** don't trim trailing slash from issuer URL ([#79](https://github.com/agjmills/trove/issues/79)) ([dfee7ec](https://github.com/agjmills/trove/commit/dfee7ec3a9067e75b5480afbb183af36fb94df9c))
* **shares:** address coderabbit review feedback ([#80](https://github.com/agjmills/trove/issues/80)) ([2c8f86d](https://github.com/agjmills/trove/commit/2c8f86d17989dc237bd953a48bd6422fb24e8c8c))

## [0.8.0](https://github.com/agjmills/trove/compare/v0.7.2...v0.8.0) (2026-04-05)


### Features

* file sharing links ([#77](https://github.com/agjmills/trove/issues/77)) ([a53bdd5](https://github.com/agjmills/trove/commit/a53bdd5255518a4cf9efa135939831e403d72840))

## [0.7.2](https://github.com/agjmills/trove/compare/v0.7.1...v0.7.2) (2026-04-05)


### Bug Fixes

* inject version info into docker image via build args ([#75](https://github.com/agjmills/trove/issues/75)) ([d5d72b3](https://github.com/agjmills/trove/commit/d5d72b37562808a299d69b7000350fa9bd4e8638))

## [0.7.1](https://github.com/agjmills/trove/compare/v0.7.0...v0.7.1) (2026-04-05)


### Bug Fixes

* build docker images separately from goreleaser ([#72](https://github.com/agjmills/trove/issues/72)) ([fc26e26](https://github.com/agjmills/trove/commit/fc26e26a007c9b6c77f2b6dc317bd79069f162ed))

## [0.7.0](https://github.com/agjmills/trove/compare/v0.6.4...v0.7.0) (2026-04-05)


### Features

* add OIDC/SSO authentication ([#70](https://github.com/agjmills/trove/issues/70)) ([0fc4b96](https://github.com/agjmills/trove/commit/0fc4b963f3773e49b3532a6994172ccbfe38b159))

## Changelog
