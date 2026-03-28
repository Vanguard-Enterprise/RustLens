export const ComponentType = {
  StringSelect: 3,
  TextInput: 4,
  Label: 18,
  RadioGroup: 21,
  CheckboxGroup: 22,
} as const;

export type ComponentTypeValue =
  (typeof ComponentType)[keyof typeof ComponentType];

export const TextInputStyle = {
  Short: 1,
  Paragraph: 2,
} as const;

export type TextInputStyleValue =
  (typeof TextInputStyle)[keyof typeof TextInputStyle];

export type SelectOption = {
  label: string;
  value: string;
  description?: string;
  default?: boolean;
};

export type APITextInput = {
  type: typeof ComponentType.TextInput;
  custom_id: string;
  style: TextInputStyleValue;
  placeholder?: string;
  max_length?: number;
  required?: boolean;
  value?: string;
  label?: string;
};

export type APIRadioGroup = {
  type: typeof ComponentType.RadioGroup;
  custom_id: string;
  options: SelectOption[];
};

export type APICheckboxGroup = {
  type: typeof ComponentType.CheckboxGroup;
  custom_id: string;
  required?: boolean;
  min_values?: number;
  max_values?: number;
  options: SelectOption[];
};

export type APIStringSelect = {
  type: typeof ComponentType.StringSelect;
  custom_id: string;
  required?: boolean;
  placeholder?: string;
  min_values?: number;
  max_values?: number;
  options: SelectOption[];
};

export type APIComponent =
  | APITextInput
  | APIRadioGroup
  | APICheckboxGroup
  | APIStringSelect;

export type APILabel = {
  type: typeof ComponentType.Label;
  label: string;
  component: APIComponent;
};

export type APIModalV2 = {
  title: string;
  custom_id: string;
  components: APILabel[];
};

export type ToJSON<T> = {
  toJSON(): T;
};
