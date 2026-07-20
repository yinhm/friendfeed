import React from 'react';
import { withRef, withVariants } from '@udecode/cn';
import { PlateElement } from 'platejs/react';
import { cva } from 'class-variance-authority';

const listVariants = cva('m-0 ps-6', {
  variants: {
    variant: {
      ul: 'list-disc [&_ul]:list-[circle] [&_ul_ul]:list-[square]',
      ol: 'list-decimal',
    },
  },
});

const ListElementVariants = withVariants(PlateElement, listVariants, [
  'variant',
]);

export const ListElement = withRef<typeof ListElementVariants>(
  ({ className, children, variant = 'ul', ...props }, ref) => {
    return (
      <ListElementVariants ref={ref} as={variant ?? 'ul'} variant={variant} {...props}>
        {children}
      </ListElementVariants>
    );
  }
);
