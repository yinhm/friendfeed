import * as React from 'react';

import {
  useLinkToolbarButton,
  useLinkToolbarButtonState,
} from '@platejs/link/react';

import { Icons } from 'components/icons';

import { ToolbarButton } from './toolbar';

export function LinkToolbarButton(
  props: React.ComponentProps<typeof ToolbarButton>
) {
  const state = useLinkToolbarButtonState();
  const { props: buttonProps } = useLinkToolbarButton(state);

  return (
    <ToolbarButton tooltip="Link" {...buttonProps} {...props}>
      <Icons.link />
    </ToolbarButton>
  );
}
