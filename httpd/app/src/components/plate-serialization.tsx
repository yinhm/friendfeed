import { createStaticEditor, serializeHtml, SlateEditor } from 'platejs';
import type { AnyPlatePlugin } from 'platejs/react';

import { components } from './static-components';
import { plugins } from './plate-plugins';

const staticPlugins = (plugins as unknown as AnyPlatePlugin[]).map((plugin) =>
  plugin.extend({ render: { afterEditable: null, beforeEditable: null } })
);

export const serializeEditorHtml = (editor: SlateEditor) => {
  const staticEditor = createStaticEditor({
    components,
    plugins: staticPlugins,
    value: editor.children,
  });
  return serializeHtml(staticEditor, {
    stripClassNames: true,
    stripDataAttributes: true,
  });
};
