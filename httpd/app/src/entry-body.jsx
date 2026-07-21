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
 */

/** @param {EntryBodyProps} props */
export function EntryBody({ rawBody, body }) {
  const value = parseValue(rawBody);
  if (value) {
    return (
      <EntryErrorBoundary fallback={body}>
        <div className="content">
          {value.map((node, index) => (
            <ValueNodeView key={index} node={node} />
          ))}
        </div>
      </EntryErrorBoundary>
    );
  }
  return (
    <div className="content" dangerouslySetInnerHTML={{ __html: body }} />
  );
}
