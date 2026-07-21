// @ts-check

import React from 'react';

import { components } from './components/static-components';

/**
 * Render a stored `rawBody` (Plate Value JSON) with the same static, URL-gated
 * component map used by HTML serialization. Entries without a rawBody fall
 * back to the server-sanitized HTML body.
 */

const MARK_KEYS = [
  'bold',
  'code',
  'highlight',
  'italic',
  'strikethrough',
  'subscript',
  'superscript',
  'underline',
];

/**
 * @typedef {{text?: string, type?: string, children?: ValueNode[],
 * [key: string]: any}} ValueNode
 */

/** @param {{node: ValueNode}} props */
function TextNode({ node }) {
  let children = /** @type {React.ReactNode} */ (node.text ?? '');
  for (const mark of MARK_KEYS) {
    if (node[mark]) {
      const Leaf = components[mark];
      children = <Leaf>{children}</Leaf>;
    }
  }
  return <>{children}</>;
}

/** @param {{node: ValueNode}} props */
function ElementNode({ node }) {
  const Component = components[node.type ?? ''] ?? components.p;
  const children = (node.children ?? []).map((child, index) => (
    <ValueNodeView key={index} node={child} />
  ));
  return <Component element={node}>{children}</Component>;
}

/** @param {{node: ValueNode}} props */
function ValueNodeView({ node }) {
  if (typeof node.text === 'string') {
    return <TextNode node={node} />;
  }
  return <ElementNode node={node} />;
}

/**
 * @param {string | undefined} rawBody
 * @returns {ValueNode[] | null}
 */
function parseValue(rawBody) {
  if (!rawBody) return null;
  try {
    const value = JSON.parse(rawBody);
    return Array.isArray(value) && value.length > 0 ? value : null;
  } catch {
    return null;
  }
}

/**
 * @typedef {object} EntryBodyProps
 * @property {string} [rawBody]
 * @property {string} body
 */

/** @param {EntryBodyProps} props */
export function EntryBody({ rawBody, body }) {
  const value = parseValue(rawBody);
  if (value) {
    return (
      <div className="content">
        {value.map((node, index) => (
          <ValueNodeView key={index} node={node} />
        ))}
      </div>
    );
  }
  return (
    <div className="content" dangerouslySetInnerHTML={{ __html: body }} />
  );
}
