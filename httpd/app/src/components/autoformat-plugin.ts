/* Markdown-style input rules: the Plate 53 successor to @platejs/autoformat.
 *
 * v53 deprecated AutoformatPlugin into an inert compatibility shim and moved
 * rule ownership to the feature plugins. Block rules (headings, blockquote,
 * code block, indent lists) are attached to their owning plugins in
 * plate-plugins.ts via the factories re-exported here. This module owns the
 * remaining pieces: the horizontal-rule fence rules (no hr plugin is
 * registered; the `hr` type is kept for rawBody compatibility) and the text
 * substitution rules, mapped 1:1 from the v49 autoformatSmartQuotes /
 * autoformatPunctuation / autoformatLegal / autoformatLegalHtml /
 * autoformatArrow / autoformatMath data.
 *
 * Compatibility risks found during the migration and how they were handled:
 * - v49 `enableUndoOnDelete` (Backspace restores the pre-substitution text)
 *   has no input-rules equivalent and was dropped.
 * - v49 fired the ``` fence anywhere in a block (`triggerAtBlockStart:
 *   false`); v53 `CodeBlockRules.markdown` only matches a whole-block fence.
 *   `codeBlockTrailingFenceInputRule` below restores the trailing-fence case.
 */

import { BlockquoteRules, HeadingRules } from '@platejs/basic-nodes';
import { CodeBlockRules, insertEmptyCodeBlock } from '@platejs/code-block';
import { BulletedListRules, OrderedListRules } from '@platejs/list';
import {
  createBlockStartInputRule,
  createSlatePlugin,
  createTextSubstitutionInputRule,
  type InsertTextInputRule,
  type InsertTextInputRuleContext,
  type SlateEditor,
  type TRange,
  type TextSubstitutionInputRuleConfig,
} from 'platejs';

import {
  ELEMENT_CODE_BLOCK,
  ELEMENT_DEFAULT,
  ELEMENT_HR,
  ELEMENT_PARAGRAPH,
} from 'components/plate-plugin-keys';

type SubstitutionPattern = TextSubstitutionInputRuleConfig['patterns'][number];

/**
 * Block conversions never fire inside a code block. This is the input-rule
 * equivalent of the v49 local `format` guard that skipped custom formatting
 * when the parent block was a code_block/code_line.
 */
const notInCodeBlock = ({ editor }: { editor: SlateEditor }) =>
  !editor.api.some({ match: { type: [ELEMENT_CODE_BLOCK] } });

/** `# ` … `###### `; the factory derives the prefix from the owning h1-h6 key. */
export const headingInputRule = () =>
  HeadingRules.markdown({ enabled: notInCodeBlock });

/** `> ` wraps the current block in a blockquote container (v53 shape). */
export const blockquoteInputRule = BlockquoteRules.markdown();

/** ``` fence converts a paragraph into a code block. */
export const codeBlockInputRule = CodeBlockRules.markdown({ on: 'match' });

/**
 * v49 parity: a ``` fence at the END of a non-empty block also converts
 * (the old rule set `triggerAtBlockStart: false`). `CodeBlockRules.markdown`
 * only fires when the whole block text is the fence (`matchBlockFence`), so
 * `notes``` would stay literal. This rule covers the trailing case: delete
 * the fence, keep the leading text, then insert an empty code block after it
 * and select its first code line — the v49 `insertEmptyCodeBlock(editor,
 * { defaultType: ELEMENT_DEFAULT, insertNodesOptions: { select: true } })`
 * behavior, against v53's identical transform.
 */
export const codeBlockTrailingFenceInputRule: InsertTextInputRule<{
  range: TRange;
}> = {
  enabled: notInCodeBlock,
  target: 'insertText',
  trigger: '`',
  resolve: ({ editor }: InsertTextInputRuleContext) => {
    if (!editor.selection || !editor.api.isCollapsed()) return;
    const block = editor.api.block();
    if (!block) return;
    const [, path] = block;
    const anchor = editor.selection.anchor;
    // The typed backtick must complete the fence at the end of the block.
    if (!editor.api.isEnd(anchor, path)) return;
    const blockStart = editor.api.start(path);
    const text = blockStart
      ? editor.api.string({ anchor: blockStart, focus: anchor })
      : '';
    // Whole-block fences are handled by CodeBlockRules.markdown.
    if (!text.endsWith('``') || text === '``') return;
    const fenceStart = editor.api.before(anchor, {
      distance: 2,
      unit: 'character',
    });
    if (!fenceStart) return;
    return { range: { anchor: fenceStart, focus: anchor } };
  },
  apply: ({ editor }: InsertTextInputRuleContext, match) => {
    editor.tf.delete({ at: match.range });
    insertEmptyCodeBlock(editor, {
      defaultType: ELEMENT_DEFAULT,
      insertNodesOptions: { select: true },
    });
    return true;
  },
};

/** Indent-list triggers registered on the indent ListPlugin. */
export const indentListInputRules = [
  BulletedListRules.markdown(),
  BulletedListRules.markdown({ variant: '*' }),
  OrderedListRules.markdown(),
  OrderedListRules.markdown({ variant: ')' }),
];

/**
 * `---` / `—-` / `___ ` produce an `hr` node followed by an empty paragraph.
 * Local implementation instead of HorizontalRuleRules: the upstream 53.0.0
 * factory never deletes the matched fence text, so the dashes would remain
 * inside the (unregistered, non-void) hr node and leak into rendered output.
 */
const hrInputRule = (match: RegExp | string, trigger: string) =>
  createBlockStartInputRule({
    enabled: notInCodeBlock,
    match,
    trigger,
    apply: ({ editor }, hrMatch) => {
      editor.tf.delete({ at: hrMatch.range });
      editor.tf.setNodes({ type: ELEMENT_HR });
      editor.tf.insertNodes({
        children: [{ text: '' }],
        type: ELEMENT_PARAGRAPH,
      });
      return true;
    },
  });

const hrInputRules = [hrInputRule(/^(--|—)$/, '-'), hrInputRule('___', ' ')];

// Order matters: patterns share triggers and the first candidate whose match
// points resolve wins. Keep the v49 rule order: smartQuotes, punctuation,
// legal, legalHtml, arrow, then math (comparison, equality, operation,
// fraction, superscript/subscript symbols, superscript/subscript numbers).
const substitutionPatterns: SubstitutionPattern[] = [
  // autoformatSmartQuotes
  { format: ['“', '”'], match: '"' },
  { format: ['‘', '’'], match: "'" },
  // autoformatPunctuation
  { format: '—', match: '--' },
  { format: '…', match: '...' },
  { format: '»', match: '>>' },
  { format: '«', match: '<<' },
  // autoformatLegal
  { format: '™', match: ['(tm)', '(TM)'] },
  { format: '®', match: ['(r)', '(R)'] },
  { format: '©', match: ['(c)', '(C)'] },
  // autoformatLegalHtml
  { format: '™', match: '&trade;' },
  { format: '®', match: '&reg;' },
  { format: '©', match: '&copy;' },
  { format: '§', match: '&sect;' },
  // autoformatArrow
  { format: '→', match: '->' },
  { format: '←', match: '<-' },
  { format: '⇒', match: '=>' },
  { format: '⇐', match: ['<=', '≤='] },
  // autoformatMath: comparison
  { format: '≯', match: '!>' },
  { format: '≮', match: '!<' },
  { format: '≥', match: '>=' },
  { format: '≤', match: '<=' },
  { format: '≱', match: '!>=' },
  { format: '≰', match: '!<=' },
  // autoformatMath: equality
  { format: '≠', match: '!=' },
  { format: '≡', match: '==' },
  { format: '≢', match: ['!==', '≠='] },
  { format: '≈', match: '~=' },
  { format: '≉', match: '!~=' },
  // autoformatMath: operation
  { format: '±', match: '+-' },
  { format: '‰', match: '%%' },
  { format: '‱', match: ['%%%', '‰%'] },
  { format: '÷', match: '//' },
  // autoformatMath: fraction
  { format: '½', match: '1/2' },
  { format: '⅓', match: '1/3' },
  { format: '¼', match: '1/4' },
  { format: '⅕', match: '1/5' },
  { format: '⅙', match: '1/6' },
  { format: '⅐', match: '1/7' },
  { format: '⅛', match: '1/8' },
  { format: '⅑', match: '1/9' },
  { format: '⅒', match: '1/10' },
  { format: '⅔', match: '2/3' },
  { format: '⅖', match: '2/5' },
  { format: '¾', match: '3/4' },
  { format: '⅗', match: '3/5' },
  { format: '⅜', match: '3/8' },
  { format: '⅘', match: '4/5' },
  { format: '⅚', match: '5/6' },
  { format: '⅝', match: '5/8' },
  { format: '⅞', match: '7/8' },
  // autoformatMath: superscript/subscript symbols
  { format: '°', match: '^o' },
  { format: '⁺', match: '^+' },
  { format: '⁻', match: '^-' },
  { format: '₊', match: '~+' },
  { format: '₋', match: '~-' },
  // autoformatMath: superscript numbers
  { format: '⁰', match: '^0' },
  { format: '¹', match: '^1' },
  { format: '²', match: '^2' },
  { format: '³', match: '^3' },
  { format: '⁴', match: '^4' },
  { format: '⁵', match: '^5' },
  { format: '⁶', match: '^6' },
  { format: '⁷', match: '^7' },
  { format: '⁸', match: '^8' },
  { format: '⁹', match: '^9' },
  // autoformatMath: subscript numbers
  { format: '₀', match: '~0' },
  { format: '₁', match: '~1' },
  { format: '₂', match: '~2' },
  { format: '₃', match: '~3' },
  { format: '₄', match: '~4' },
  { format: '₅', match: '~5' },
  { format: '₆', match: '~6' },
  { format: '₇', match: '~7' },
  { format: '₈', match: '~8' },
  { format: '₉', match: '~9' },
];

/**
 * Local owner for the rules that have no dedicated feature plugin: the hr
 * fence (`---`, `—-`, `___ `) and the text substitutions. The hr rules must
 * precede the substitution rule so `---` at block start beats `--` → em dash.
 */
export const MarkdownShortcutsPlugin = createSlatePlugin({
  key: 'markdownShortcuts',
  inputRules: [
    ...hrInputRules,
    createTextSubstitutionInputRule({ patterns: substitutionPatterns }),
  ],
});
