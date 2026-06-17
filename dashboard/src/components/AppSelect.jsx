import * as SelectPrimitive from '@radix-ui/react-select';
import { Check, ChevronDown, ChevronUp } from 'lucide-react';

export default function AppSelect({ value, onValueChange, options = [], placeholder, disabled, className = '', contentClassName = '' }) {
    return (
        <SelectPrimitive.Root value={value} onValueChange={onValueChange} disabled={disabled}>
            <SelectPrimitive.Trigger className={`app-select-trigger ${className}`.trim()}>
                <SelectPrimitive.Value placeholder={placeholder} />
                <SelectPrimitive.Icon asChild>
                    <ChevronDown size={16} />
                </SelectPrimitive.Icon>
            </SelectPrimitive.Trigger>

            <SelectPrimitive.Portal>
                <SelectPrimitive.Content className={`app-select-content ${contentClassName}`.trim()} position="popper" sideOffset={6}>
                    <SelectPrimitive.ScrollUpButton className="app-select-scroll-button">
                        <ChevronUp size={14} />
                    </SelectPrimitive.ScrollUpButton>

                    <SelectPrimitive.Viewport className="app-select-viewport">
                        {options.map((option) => (
                            <SelectPrimitive.Item key={option.value} value={option.value} className="app-select-item">
                                <SelectPrimitive.ItemText>{option.label}</SelectPrimitive.ItemText>
                                <SelectPrimitive.ItemIndicator className="app-select-item-indicator">
                                    <Check size={14} />
                                </SelectPrimitive.ItemIndicator>
                            </SelectPrimitive.Item>
                        ))}
                    </SelectPrimitive.Viewport>

                    <SelectPrimitive.ScrollDownButton className="app-select-scroll-button">
                        <ChevronDown size={14} />
                    </SelectPrimitive.ScrollDownButton>
                </SelectPrimitive.Content>
            </SelectPrimitive.Portal>
        </SelectPrimitive.Root>
    );
}
