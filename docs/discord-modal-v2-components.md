# Discord.js Modal V2 Components (Raw API)

discord.js doesn't have builder classes for the newer modal components yet. You can use raw API objects directly with `interaction.showModal()`.

## Component Type IDs

| Type | ID | Notes |
|------|---:|-------|
| ActionRow | 1 | Legacy wrapper (not needed with Label) |
| StringSelect | 3 | Dropdown menu |
| TextInput | 4 | Single/multi-line text |
| Label | 18 | Wraps any component with a label |
| RadioGroup | 21 | Single choice radio buttons |
| CheckboxGroup | 22 | Multiple choice checkboxes |

## Label Wrapper

Every component in a v2 modal must be wrapped in a Label (type 18):

```js
{
  type: 18,
  label: "Your label text",
  component: { /* any component */ }
}
```

## RadioGroup

Single selection from a list of options.

```js
{
  type: 18,
  label: "Category",
  component: {
    type: 21,
    custom_id: "category",
    options: [
      { label: "Bug Report", value: "bug" },
      { label: "Feature Request", value: "feature" },
      { label: "General", value: "general" },
    ],
  },
}
```

## CheckboxGroup

Multiple selections. Control min/max with `min_values` and `max_values`.

```js
{
  type: 18,
  label: "Affected areas",
  component: {
    type: 22,
    custom_id: "areas",
    min_values: 1,
    max_values: 4,
    options: [
      { label: "UI", value: "ui" },
      { label: "API", value: "api" },
      { label: "Database", value: "db" },
    ],
  },
}
```

## StringSelect (in modal)

Dropdown menu. Supports `placeholder`, `min_values`, `max_values`.

```js
{
  type: 18,
  label: "Priority",
  component: {
    type: 3,
    custom_id: "priority",
    placeholder: "Select priority...",
    options: [
      { label: "Low", value: "low" },
      { label: "Medium", value: "medium" },
      { label: "High", value: "high" },
    ],
  },
}
```

## TextInput

`style: 1` = short (single line), `style: 2` = paragraph (multi-line).

```js
{
  type: 18,
  label: "Subject",
  component: {
    type: 4,
    custom_id: "subject",
    style: 1,
    placeholder: "Brief summary...",
    max_length: 100,
    required: true,      // default: true
  },
}
```

## Full Modal Example

```js
await interaction.showModal({
  title: "Feedback Form",
  custom_id: "feedback_modal",
  components: [
    {
      type: 18,
      label: "Category",
      component: {
        type: 21,
        custom_id: "category",
        options: [
          { label: "Bug Report", value: "bug" },
          { label: "Feature Request", value: "feature" },
        ],
      },
    },
    {
      type: 18,
      label: "Priority",
      component: {
        type: 3,
        custom_id: "priority",
        placeholder: "Select...",
        options: [
          { label: "Low", value: "low" },
          { label: "High", value: "high" },
        ],
      },
    },
    {
      type: 18,
      label: "Tags",
      component: {
        type: 22,
        custom_id: "tags",
        min_values: 1,
        max_values: 3,
        options: [
          { label: "UI", value: "ui" },
          { label: "API", value: "api" },
          { label: "Auth", value: "auth" },
        ],
      },
    },
    {
      type: 18,
      label: "Description",
      component: {
        type: 4,
        custom_id: "description",
        style: 2,
        placeholder: "Details...",
        max_length: 2000,
      },
    },
  ],
});
```

## Reading Submitted Values

```js
client.on("interactionCreate", async (interaction) => {
  if (!interaction.isModalSubmit()) return;

  const fields = interaction.fields;

  // TextInput — has a dedicated getter
  const description = fields.getTextInputValue("description");

  // RadioGroup, CheckboxGroup, StringSelect — use getField()
  const category = fields.getField("category")?.value;
  const tags = fields.getField("tags")?.value;
  const priority = fields.getField("priority")?.value;
});
```
