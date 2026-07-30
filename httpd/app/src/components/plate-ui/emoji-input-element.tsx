import React, { useEffect, useMemo, useState } from 'react';

import { EmojiInlineIndexSearch, insertEmoji } from '@platejs/emoji';
import { type PlateElementProps, PlateElement } from 'platejs/react';

import {
  InlineCombobox,
  InlineComboboxContent,
  InlineComboboxEmpty,
  InlineComboboxInput,
  InlineComboboxItem,
} from './inline-combobox';

function useDebouncedValue<T>(value: T, delay: number) {
  const [debouncedValue, setDebouncedValue] = useState(value);

  useEffect(() => {
    const timeout = setTimeout(() => setDebouncedValue(value), delay);
    return () => clearTimeout(timeout);
  }, [delay, value]);

  return debouncedValue;
}

export function EmojiInputElement(props: PlateElementProps) {
  const { children, editor, element } = props;
  const [value, setValue] = useState('');
  const debouncedValue = useDebouncedValue(value, 100);
  const isPending = value !== debouncedValue;

  const filteredEmojis = useMemo(() => {
    if (debouncedValue.trim().length === 0) return [];

    return EmojiInlineIndexSearch.getInstance()
      .search(debouncedValue.replace(/:$/, ''))
      .get();
  }, [debouncedValue]);

  return (
    <PlateElement as="span" data-slate-value={element.value} {...props}>
      <InlineCombobox
        element={element}
        filter={false}
        hideWhenNoValue
        setValue={setValue}
        trigger=":"
        value={value}
      >
        <InlineComboboxInput />
        <InlineComboboxContent>
          {!isPending && (
            <InlineComboboxEmpty>No matching emoji found</InlineComboboxEmpty>
          )}
          {filteredEmojis.map((emoji) => (
            <InlineComboboxItem
              key={emoji.id}
              onClick={() => insertEmoji(editor, emoji)}
              value={emoji.name}
            >
              {emoji.skins[0].native} {emoji.name}
            </InlineComboboxItem>
          ))}
        </InlineComboboxContent>
      </InlineCombobox>
      {children}
    </PlateElement>
  );
}
