import React from 'react';
import { withRef } from 'platejs/react';
import { cn } from 'components/cn';
import { PlateElement, withHOC } from 'platejs/react';
import { Image, useMediaState } from '@platejs/media/react';
import { ResizableProvider, useResizableValue } from '@platejs/resizable';

import { Caption, CaptionTextarea } from './caption';
import { ELEMENT_IMAGE } from 'components/plate-plugin-keys';
import { MediaPopover } from './media-popover';
import {
  mediaResizeHandleVariants,
  Resizable,
  ResizeHandle,
} from './resizable';

export const ImageElement = withHOC(
  ResizableProvider,
  withRef<typeof PlateElement>(
    ({ className, children, ...props }, ref) => {
      const { readOnly, focused, selected, align = 'center' } = useMediaState();
      const width = useResizableValue('width');

      return (
        <MediaPopover pluginKey={ELEMENT_IMAGE}>
          <PlateElement
            ref={ref}
            className={cn('py-2.5', className)}
            {...props}
          >
            <figure className="group relative m-0" contentEditable={false}>
              <Resizable
                align={align}
                options={{
                  align,
                  readOnly,
                }}
              >
                <ResizeHandle
                  options={{ direction: 'left' }}
                  className={mediaResizeHandleVariants({ direction: 'left' })}
                />
                <Image
                  className={cn(
                    'block w-full max-w-full cursor-pointer object-cover px-0',
                    'rounded-sm',
                    focused && selected && 'ring-2 ring-ring ring-offset-2'
                  )}
                  alt=""
                />
                <ResizeHandle
                  options={{ direction: 'right' }}
                  className={mediaResizeHandleVariants({ direction: 'right' })}
                />
              </Resizable>

              <Caption align={align} style={{ width }}>
                <CaptionTextarea
                  placeholder="Write a caption..."
                  readOnly={readOnly}
                />
              </Caption>
            </figure>

            {children}
          </PlateElement>
        </MediaPopover>
      );
    }
  )
);
