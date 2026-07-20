import {
  createPluginFactory,
  HotkeyPlugin,
  onKeyDownToggleElement,
} from '@udecode/plate-common';

import { ELEMENT_PARAGRAPH } from 'components/plate-plugin-keys';

// plate-paragraph stops at v36. Keep its behavior local until the plugin is
// replaced by ParagraphPlugin during the Plate object-API migration.
export const createParagraphPlugin = createPluginFactory<HotkeyPlugin>({
  deserializeHtml: {
    query: (element) => element.style.fontFamily !== 'Consolas',
    rules: [{ validNodeName: 'P' }],
  },
  handlers: {
    onKeyDown: onKeyDownToggleElement,
  },
  isElement: true,
  key: ELEMENT_PARAGRAPH,
  options: {
    hotkey: ['mod+opt+0', 'mod+shift+0'],
  },
});
