// @ts-check

import React from 'react';

/** @typedef {{url: string, name: string, type?: string, size?: number}} EntryFile */

/** @param {number|undefined} size */
export function formatFileSize(size) {
  if (!size) return '';
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

/** @param {{files?: EntryFile[]}} props */
export function EntryFiles({ files }) {
  if (!files?.length) return null;
  return (
    <div className="entry-files" aria-label="Attachments">
      {files.map((file, index) => (
        <a className="entry-file" href={file.url} download={file.name} key={`${file.url}-${index}`}>
          <span className="entry-file-icon" aria-hidden="true" />
          <span>{file.name}</span>
          {file.size ? <span className="entry-file-size">{formatFileSize(file.size)}</span> : null}
        </a>
      ))}
    </div>
  );
}
