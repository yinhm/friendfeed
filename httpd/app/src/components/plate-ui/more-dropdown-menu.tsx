import React from 'react';
import { DropdownMenuProps } from '@radix-ui/react-dropdown-menu';
import { useEditorRef } from 'platejs/react';

import { Icons } from 'components/icons';
import { MARK_SUBSCRIPT, MARK_SUPERSCRIPT } from 'components/plate-plugin-keys';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  useOpenState,
} from './dropdown-menu';
import { ToolbarButton } from './toolbar';

export function MoreDropdownMenu(props: DropdownMenuProps) {
  const editor = useEditorRef();
  const openState = useOpenState();

  return (
    <DropdownMenu modal={false} {...openState} {...props}>
      <DropdownMenuTrigger asChild>
        <ToolbarButton pressed={openState.open} tooltip="Insert">
          <Icons.more />
        </ToolbarButton>
      </DropdownMenuTrigger>

      <DropdownMenuContent
        align="start"
        className="flex max-h-[500px] min-w-[180px] flex-col gap-0.5 overflow-y-auto"
      >
        <DropdownMenuItem
          onSelect={() => {
            editor.tf.removeMark(MARK_SUPERSCRIPT);
            editor.tf.toggleMark(MARK_SUBSCRIPT);
            editor.tf.focus();
          }}
        >
          <Icons.superscript className="mr-2 h-5 w-5" />
          Superscript
          {/* (⌘+,) */}
        </DropdownMenuItem>
        <DropdownMenuItem
          onSelect={() => {
            editor.tf.removeMark(MARK_SUBSCRIPT);
            editor.tf.toggleMark(MARK_SUPERSCRIPT);
            editor.tf.focus();
          }}
        >
          <Icons.subscript className="mr-2 h-5 w-5" />
          Subscript
          {/* (⌘+.) */}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
