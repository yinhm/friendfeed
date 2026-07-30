import * as React from 'react';

import {
  useListToolbarButton,
  useListToolbarButtonState,
} from '@platejs/list-classic/react';

import { Icons } from 'components/icons';
import { ELEMENT_UL } from 'components/plate-plugin-keys';

import { ToolbarButton } from './toolbar';

export function ListToolbarButton({
  nodeType = ELEMENT_UL,
  ...props
}: React.ComponentProps<typeof ToolbarButton> & {
  nodeType?: string;
}) {
  const state = useListToolbarButtonState({ nodeType });
  const { props: buttonProps } = useListToolbarButton(state);

  return (
    <ToolbarButton
      tooltip={nodeType === ELEMENT_UL ? 'Bulleted List' : 'Numbered List'}
      {...buttonProps}
      {...props}
    >
      {nodeType === ELEMENT_UL ? <Icons.ul /> : <Icons.ol />}
    </ToolbarButton>
  );
}
