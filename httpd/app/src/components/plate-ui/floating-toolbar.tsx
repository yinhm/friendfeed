'use client';

import * as React from 'react';

import {
  type FloatingToolbarState,
  flip,
  offset,
  useFloatingToolbar,
  useFloatingToolbarState,
} from '@platejs/floating';
import {
  PortalBody,
  useComposedRef,
  useEditorId,
  useEventEditorValue,
} from 'platejs/react';

import { cn } from 'components/cn';

import { Toolbar } from './toolbar';

export function FloatingToolbar({
  children,
  className,
  state,
  ref,
  ...props
}: React.ComponentProps<typeof Toolbar> & {
  state?: FloatingToolbarState;
}) {
  const editorId = useEditorId();
  const focusedEditorId = useEventEditorValue('focus');
  const floatingToolbarState = useFloatingToolbarState({
    editorId,
    focusedEditorId,
    ...state,
    floatingOptions: {
      placement: 'top',
      middleware: [
        offset(12),
        flip({
          padding: 12,
          fallbackPlacements: [
            'top-start',
            'top-end',
            'bottom-start',
            'bottom-end',
          ],
        }),
      ],
      ...state?.floatingOptions,
    },
  });

  const {
    ref: floatingRef,
    props: rootProps,
    hidden,
  } = useFloatingToolbar(floatingToolbarState);

  const composedRef = useComposedRef<HTMLDivElement>(ref, floatingRef);

  if (hidden) return null;

  return (
    <PortalBody>
      <Toolbar
        ref={composedRef}
        className={cn(
          'absolute z-50 whitespace-nowrap border bg-popover px-0.5 opacity-100 shadow-md print:hidden',
          className
        )}
        {...rootProps}
        {...props}
      >
        {children}
      </Toolbar>
    </PortalBody>
  );
}
