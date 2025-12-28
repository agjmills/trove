/**
 * Chunked Upload Manager for resumable file uploads
 * Handles file chunking, upload retry, pause/resume, and cancellation
 */

class ChunkedUploadManager {
	constructor(config = {}) {
		this.chunkSize = config.chunkSize || 5 * 1024 * 1024; // 5MB default
		this.maxRetries = config.maxRetries || 3;
		this.uploadSessions = new Map(); // Track active upload sessions
		this.onProgress = config.onProgress || (() => {});
		this.onComplete = config.onComplete || (() => {});
		this.onError = config.onError || (() => {});
		this.onCancel = config.onCancel || (() => {});
	}

	/**
	 * Start a chunked upload
	 * @param {File} file - File to upload
	 * @param {Object} options - Upload options (folder, etc.)
	 * @returns {Promise<string>} uploadId
	 */
	async startUpload(file, options = {}) {
		const { folder = '/', onProgress, onComplete, onError, onCancel } = options;

		// Calculate chunks
		const totalChunks = Math.ceil(file.size / this.chunkSize);

		// Calculate file hash (optional but recommended for integrity)
		let fileHash = '';
		try {
			fileHash = await this.calculateFileHash(file);
		} catch (err) {
			console.warn('Failed to calculate file hash:', err);
		}

		// Initialize upload session with server
		const initResponse = await fetch('/api/uploads/init', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			credentials: 'same-origin',
			body: JSON.stringify({
				filename: file.name,
				total_size: file.size,
				chunk_size: this.chunkSize,
				total_chunks: totalChunks,
				logical_path: folder,
				mime_type: file.type || 'application/octet-stream',
				hash: fileHash,
			}),
		});

		if (!initResponse.ok) {
			const error = await initResponse.text();
			throw new Error(error || 'Failed to initialize upload');
		}

		const { upload_id, chunks_received } = await initResponse.json();

		// Create upload session
		const session = {
			uploadId: upload_id,
			file,
			folder,
			totalChunks,
			uploadedChunks: new Set(chunks_received || []),
			isPaused: false,
			isCancelled: false,
			currentChunk: 0,
			retries: new Map(), // Track retries per chunk
			onProgress: onProgress || this.onProgress,
			onComplete: onComplete || this.onComplete,
			onError: onError || this.onError,
			onCancel: onCancel || this.onCancel,
		};

		this.uploadSessions.set(upload_id, session);

		// Start uploading chunks
		this.uploadChunks(upload_id);

		return upload_id;
	}

	/**
	 * Upload chunks sequentially with retry logic
	 */
	async uploadChunks(uploadId) {
		const session = this.uploadSessions.get(uploadId);
		if (!session) return;

		for (let chunkNum = 0; chunkNum < session.totalChunks; chunkNum++) {
			// Check if paused or cancelled
			if (session.isPaused) {
				return; // Will resume later
			}
			if (session.isCancelled) {
				return;
			}

			// Skip already uploaded chunks
			if (session.uploadedChunks.has(chunkNum)) {
				this.notifyProgress(session, chunkNum);
				continue;
			}

			session.currentChunk = chunkNum;

			// Upload chunk with retry
			let success = false;
			let retries = 0;

			while (!success && retries <= this.maxRetries) {
				try {
					await this.uploadSingleChunk(uploadId, chunkNum);
					session.uploadedChunks.add(chunkNum);
					success = true;
					this.notifyProgress(session, chunkNum);
				} catch (err) {
					retries++;
					if (retries > this.maxRetries) {
						session.onError(new Error(`Failed to upload chunk ${chunkNum} after ${this.maxRetries} retries`));
						try {
							await this.cancelUpload(uploadId);
						} catch (cancelErr) {
							session.onError(cancelErr);
						}
						return;
					}
					// Wait before retry (exponential backoff)
					await this.sleep(Math.min(1000 * Math.pow(2, retries - 1), 10000));
				}
			}
		}

		// All chunks uploaded, complete the upload
		try {
			await this.completeUpload(uploadId);
		} catch (err) {
			session.onError(err);
		}
	}

	/**
	 * Upload a single chunk
	 */
	async uploadSingleChunk(uploadId, chunkNum) {
		const session = this.uploadSessions.get(uploadId);
		if (!session) throw new Error('Session not found');

		const start = chunkNum * this.chunkSize;
		const end = Math.min(start + this.chunkSize, session.file.size);
		const chunk = session.file.slice(start, end);

		const response = await fetch(`/api/uploads/${uploadId}/chunk?chunk=${chunkNum}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/octet-stream' },
			credentials: 'same-origin',
			body: chunk,
		});

		if (!response.ok) {
			const error = await response.text();
			throw new Error(error || `Failed to upload chunk ${chunkNum}`);
		}

		return await response.json();
	}

	/**
	 * Complete the upload after all chunks are uploaded
	 */
	async completeUpload(uploadId) {
		const session = this.uploadSessions.get(uploadId);
		if (!session) throw new Error('Session not found');

		const response = await fetch(`/api/uploads/${uploadId}/complete`, {
			method: 'POST',
			credentials: 'same-origin',
		});

		if (!response.ok) {
			const error = await response.text();
			throw new Error(error || 'Failed to complete upload');
		}

		const result = await response.json();
		try {
			await session.onComplete(result);
		} finally {
			this.uploadSessions.delete(uploadId);
		}
		return result;
	}

	/**
	 * Pause an upload
	 */
	pauseUpload(uploadId) {
		const session = this.uploadSessions.get(uploadId);
		if (session) {
			session.isPaused = true;
		}
	}

	/**
	 * Resume a paused upload
	 */
	resumeUpload(uploadId) {
		const session = this.uploadSessions.get(uploadId);
		if (session && session.isPaused) {
			session.isPaused = false;
			this.uploadChunks(uploadId);
		}
	}

	/**
	 * Cancel an upload
	 */
	async cancelUpload(uploadId) {
		const session = this.uploadSessions.get(uploadId);
		if (!session) {
			// Fallback to class-level handler if session not found
			this.onCancel(uploadId);
			return;
		}

		session.isCancelled = true;

		// Notify server
		try {
			await fetch(`/api/uploads/${uploadId}`, {
				method: 'DELETE',
				credentials: 'same-origin',
			});
		} catch (err) {
			console.error('Failed to cancel upload on server:', err);
		}

		const cancelCallback = session.onCancel || this.onCancel;
		cancelCallback(uploadId);
		this.uploadSessions.delete(uploadId);
	}

	/**
	 * Get upload status
	 */
	async getStatus(uploadId) {
		const response = await fetch(`/api/uploads/${uploadId}/status`, {
			method: 'GET',
			credentials: 'same-origin',
		});

		if (!response.ok) {
			throw new Error('Failed to get upload status');
		}

		return await response.json();
	}

	/**
	 * Notify progress callback
	 */
	notifyProgress(session, chunkNum) {
		const percentage = Math.round(((chunkNum + 1) / session.totalChunks) * 100);
		session.onProgress({
			uploadId: session.uploadId,
			filename: session.file.name,
			uploadedChunks: session.uploadedChunks.size,
			totalChunks: session.totalChunks,
			percentage,
			uploadedBytes: Math.min(session.uploadedChunks.size * this.chunkSize, session.file.size),
			totalBytes: session.file.size,
		});
	}

	/**
	 * Calculate SHA-256 hash of entire file
	 */
	async calculateFileHash(file) {
		// Use streaming approach for large files
		const chunkSize = 64 * 1024 * 1024; // 64MB chunks for hashing
		const chunks = Math.ceil(file.size / chunkSize);
		
		// For files under 100MB, use simple approach
		if (file.size < 100 * 1024 * 1024) {
			const hashDigest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
			return Array.from(new Uint8Array(hashDigest))
				.map(b => b.toString(16).padStart(2, '0'))
				.join('');
		}
		
		// For larger files, process in chunks using a stream
		const stream = file.stream();
		const reader = stream.getReader();
		const hashBuffer = [];
		
		while (true) {
			const { done, value } = await reader.read();
			if (done) break;
			hashBuffer.push(value);
		}
		
		const combined = new Uint8Array(hashBuffer.reduce((acc, arr) => acc + arr.length, 0));
		let offset = 0;
		for (const arr of hashBuffer) {
			combined.set(arr, offset);
			offset += arr.length;
		}
		
		const hashDigest = await crypto.subtle.digest('SHA-256', combined);
		return Array.from(new Uint8Array(hashDigest))
			.map(b => b.toString(16).padStart(2, '0'))
			.join('');
	}

	/**
	 * Sleep utility
	 */
	sleep(ms) {
		return new Promise(resolve => setTimeout(resolve, ms));
	}

	/**
	 * Check if upload is in progress
	 */
	isUploading(uploadId) {
		return this.uploadSessions.has(uploadId);
	}

	/**
	 * Get all active uploads
	 */
	getActiveUploads() {
		return Array.from(this.uploadSessions.keys());
	}
}

// Export for use in templates
if (typeof window !== 'undefined') {
	window.ChunkedUploadManager = ChunkedUploadManager;
}
