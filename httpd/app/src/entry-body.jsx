// @ts-check

import React from 'react';

/**
 * Entry bodies render the server-sanitized HTML body as-is: it is generated
 * from the Plate value at save time, truncated with a "Read more..." link on
 * feed lists and kept whole on the entry page, and list pages already have
 * media-box images collapsed out of it (httpd/src/feed.go). The Plate
 * rawBody is editor data and never drives display.
 *
 * @typedef {object} EntryBodyProps
 * @property {string} body
 */

/** @param {EntryBodyProps} props */
export function EntryBody({ body }) {
  return <div className="content" dangerouslySetInnerHTML={{ __html: body }} />;
}
