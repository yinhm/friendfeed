import React from 'react';
import { cn } from 'components/cn';
import { ELEMENT_H1, ELEMENT_PARAGRAPH } from 'components/plate-plugin-keys';

type PlaceholderProps = {
  children: React.ReactElement | React.ReactElement[];
  placeholder: string;
  nodeProps?: Record<string, unknown>;
  enabled?: boolean;
};

export const Placeholder = (props: PlaceholderProps) => {
  const { children, placeholder, nodeProps } = props;
  const enabled = props.enabled ?? true;

  return React.Children.map(children, (child) => {
    const element = child as React.ReactElement<any>;
    return React.cloneElement(element, {
      className: element.props.className,
      nodeProps: {
        ...nodeProps,
        className: cn(
          enabled &&
            'before:absolute before:cursor-text before:opacity-30 before:content-[attr(placeholder)]'
        ),
        placeholder,
      },
    });
  });
};

export const withPlaceholder = (Component: React.ComponentType<any>) =>
  function PlaceholderComponent(props: any) {
    return (
      <Placeholder {...props}>
        <Component {...props} />
      </Placeholder>
    );
  };

export const withPlaceholdersPrimitive = (components: any, _options?: any) =>
  components;

export const withPlaceholders = (components: any) =>
  withPlaceholdersPrimitive(components, [
    {
      key: ELEMENT_PARAGRAPH,
      placeholder: 'Type a paragraph',
      hideOnBlur: true,
      query: {
        maxLevel: 1,
      },
    },
    {
      key: ELEMENT_H1,
      placeholder: 'Untitled',
      hideOnBlur: false,
    },
  ]);
