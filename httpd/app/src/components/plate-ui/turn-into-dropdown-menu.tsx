import React from 'react';
import { DropdownMenuProps } from '@radix-ui/react-dropdown-menu';
import { TElement } from 'platejs';
import { useEditorRef, useEditorSelector } from 'platejs/react';

import { Icons } from 'components/icons';
import {
  ELEMENT_BLOCKQUOTE,
  ELEMENT_H1,
  ELEMENT_H2,
  ELEMENT_H3,
  ELEMENT_PARAGRAPH,
} from 'components/plate-plugin-keys';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
  useOpenState,
} from './dropdown-menu';
import { ToolbarButton } from './toolbar';

const items = [
  {
    value: ELEMENT_PARAGRAPH,
    label: 'Paragraph',
    description: 'Paragraph',
    icon: Icons.paragraph,
  },
  {
    value: ELEMENT_H1,
    label: 'Heading 1',
    description: 'Heading 1',
    icon: Icons.h1,
  },
  {
    value: ELEMENT_H2,
    label: 'Heading 2',
    description: 'Heading 2',
    icon: Icons.h2,
  },
  {
    value: ELEMENT_H3,
    label: 'Heading 3',
    description: 'Heading 3',
    icon: Icons.h3,
  },
  {
    value: ELEMENT_BLOCKQUOTE,
    label: 'Quote',
    description: 'Quote (⌘+⇧+.)',
    icon: Icons.blockquote,
  },
  // {
  //   value: 'ul',
  //   label: 'Bulleted list',
  //   description: 'Bulleted list',
  //   icon: Icons.ul,
  // },
  // {
  //   value: 'ol',
  //   label: 'Numbered list',
  //   description: 'Numbered list',
  //   icon: Icons.ol,
  // },
];

const defaultItem = items.find((item) => item.value === ELEMENT_PARAGRAPH)!;

export function TurnIntoDropdownMenu(props: DropdownMenuProps) {
  const value: string = useEditorSelector((editor) => {
    if (editor.api.isCollapsed()) {
      const entry = editor.api.node<TElement>({
        match: (n) => editor.api.isBlock(n),
      });

      if (entry) {
        return (
          items.find((item) => item.value === entry[0].type)?.value ??
          ELEMENT_PARAGRAPH
        );
      }
    }

    return ELEMENT_PARAGRAPH;
  }, []);

  const editor = useEditorRef();
  const openState = useOpenState();

  const selectedItem =
    items.find((item) => item.value === value) ?? defaultItem;
  const { icon: SelectedItemIcon, label: selectedItemLabel } = selectedItem;

  return (
    <DropdownMenu modal={false} {...openState} {...props}>
      <DropdownMenuTrigger asChild>
        <ToolbarButton
          pressed={openState.open}
          tooltip="Turn into"
          isDropdown
          className="lg:min-w-[130px]"
        >
          <SelectedItemIcon className="h-4 w-4 lg:hidden" />
          <span className="max-lg:hidden">{selectedItemLabel}</span>
        </ToolbarButton>
      </DropdownMenuTrigger>

      <DropdownMenuContent align="start" className="min-w-0">
        <DropdownMenuLabel>Turn into</DropdownMenuLabel>

        <DropdownMenuRadioGroup
          className="flex flex-col gap-0.5"
          value={value}
          onValueChange={(type) => {
            // if (type === 'ul' || type === 'ol') {
            //   if (settingsStore.get.checkedId(KEY_LIST_STYLE_TYPE)) {
            //     toggleIndentList(editor, {
            //       listStyleType: type === 'ul' ? 'disc' : 'decimal',
            //     });
            //   } else if (settingsStore.get.checkedId('list')) {
            //     toggleList(editor, { type });
            //   }
            // } else {
            //   unwrapList(editor);
            editor.tf.setNodes({ type });
            // }

            editor.tf.collapse();
            editor.tf.focus();
          }}
        >
          {items.map(({ value: itemValue, label, icon: Icon }) => (
            <DropdownMenuRadioItem
              key={itemValue}
              value={itemValue}
              className="min-w-[180px]"
            >
              <Icon className="mr-2 h-5 w-5" />
              {label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
