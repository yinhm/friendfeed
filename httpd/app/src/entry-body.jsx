// @ts-check

import React from 'react';

import { components } from './components/static-components';

/**
 * Render a stored `rawBody` (Plate Value JSON) with the same static, URL-gated
 * component map used by HTML serialization. Malformed values — rawBody is
 * client-supplied — fall back to the server-sanitized HTML body, so a single
 * bad entry can never crash the whole feed.
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

/**
 * Feed lists truncate entry bodies at this many characters of text content,
 * matching the server-side body truncation (httpd/src/feed.go). The entry
 * page itself always renders the full value.
 */
export const TRUNCATE_AT = 300;

/**
 * @param {ValueNode} node
 * @returns {number}
 */
function textLength(node) {
  if (typeof node.text === 'string') return node.text.length;
  return (node.children ?? []).reduce((n, child) => n + textLength(child), 0);
}

/**
 * Cut a node subtree at `remaining` characters without mutating the input.
 * @param {ValueNode} node
 * @param {number} remaining
 * @returns {[ValueNode, boolean]} the (possibly cut) node and whether anything was dropped
 */
function truncateNode(node, remaining) {
  if (typeof node.text === 'string') {
    if (node.text.length <= remaining) return [node, false];
    return [{ ...node, text: node.text.slice(0, remaining) }, true];
  }
  const children = [];
  let left = remaining;
  let hit = false;
  for (const child of node.children ?? []) {
    if (left <= 0) {
      hit = true;
      break;
    }
    const length = textLength(child);
    if (length <= left) {
      children.push(child);
      left -= length;
    } else {
      const [cut] = truncateNode(child, left);
      if (cut) children.push(cut);
      hit = true;
      break;
    }
  }
  return [{ ...node, children }, hit];
}

/**
 * Truncate a Plate value to `maxChars` characters of text content.
 * @param {ValueNode[]} nodes
 * @param {number} maxChars
 * @returns {{nodes: ValueNode[], truncated: boolean}}
 */
export function truncateValue(nodes, maxChars) {
  const out = [];
  let left = maxChars;
  let truncated = false;
  for (const node of nodes) {
    if (left <= 0) {
      truncated = true;
      break;
    }
    const length = textLength(node);
    if (length <= left) {
      out.push(node);
      left -= length;
    } else {
      const [cut] = truncateNode(node, left);
      if (cut) out.push(cut);
      truncated = true;
      break;
    }
  }
  return { nodes: out, truncated };
}

/**
 * Recursively validate a value node: text nodes need a string `text`;
 * element nodes need a `type` and either no children or an array of valid
 * children. Anything else (null, scalars, children that is not an array)
 * is rejected.
 * @param {unknown} node
 * @returns {boolean}
 */
function isValidNode(node) {
  if (!node || typeof node !== 'object' || Array.isArray(node)) return false;
  const candidate = /** @type {ValueNode} */ (node);
  if (candidate.text !== undefined) {
    return typeof candidate.text === 'string';
  }
  if (typeof candidate.type === 'string') {
    return (
      candidate.children === undefined ||
      (Array.isArray(candidate.children) && candidate.children.every(isValidNode))
    );
  }
  return false;
}

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
    if (!Array.isArray(value) || value.length === 0) return null;
    return value.every(isValidNode) ? value : null;
  } catch {
    return null;
  }
}

/**
 * Last-resort guard: even a validated value could hit an unexpected node
 * shape at render time, so rendering errors degrade to the sanitized HTML
 * body instead of taking the feed down.
 *
 * @typedef {object} EntryErrorBoundaryProps
 * @property {string} fallback
 * @property {React.ReactNode} [children]
 *
 * @extends {React.Component<EntryErrorBoundaryProps, {failed: boolean}>}
 */
class EntryErrorBoundary extends React.Component {
  /** @param {EntryErrorBoundaryProps} props */
  constructor(props) {
    super(props);
    this.state = { failed: false };
  }

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (this.state.failed) {
      return (
        <div
          className="content"
          dangerouslySetInnerHTML={{ __html: this.props.fallback }}
        />
      );
    }
    return this.props.children;
  }
}

/**
 * @typedef {object} EntryBodyProps
 * @property {string} [rawBody]
 * @property {string} body
 * @property {boolean} [truncate] truncate feed-list bodies at 300 chars of text
 * @property {string} [entryId] target of the "Read more..." link when truncated
 */

/** @param {EntryBodyProps} props */
export function EntryBody({ rawBody, body, truncate, entryId }) {
  const value = parseValue(rawBody);
  if (value) {
    const { nodes, truncated } = truncate
      ? truncateValue(value, TRUNCATE_AT)
      : { nodes: value, truncated: false };
    return (
      <EntryErrorBoundary fallback={body}>
        <div className="content">
          {nodes.map((node, index) => (
            <ValueNodeView key={index} node={node} />
          ))}
          {truncated && (
            <a href={`/e/${entryId}`} style={{ paddingLeft: '30px' }}>
              Read more...
            </a>
          )}
        </div>
      </EntryErrorBoundary>
    );
  }
  return (
    <div className="content" dangerouslySetInnerHTML={{ __html: body }} />
  );
}
