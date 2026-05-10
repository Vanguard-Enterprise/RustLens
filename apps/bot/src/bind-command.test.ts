import { describe, expect, it } from "vitest";
import { __testables } from "./bind-command.js";
import { ComponentType, TextInputStyle } from "./builders/index.js";

describe("bind command", () => {
  it("builds the first fullkit modal within Discord's 5 component limit", () => {
    const modal = __testables.buildBindFullkitStartModal().toJSON();

    expect(modal.title).toBe("Bind Fullkit");
    expect(modal.custom_id).toBe("bind_fullkit_modal_step_1");
    expect(modal.components).toHaveLength(5);
    expect(modal.components).toEqual([
      {
        type: ComponentType.Label,
        label: "Weapon",
        component: {
          type: ComponentType.RadioGroup,
          custom_id: "bind_fullkit_weapon",
          options: [
            { label: "Assault Rifle", value: "ak" },
            { label: "MP5A4", value: "mp5" },
            { label: "Thompson", value: "thompson" },
          ],
        },
      },
      {
        type: ComponentType.Label,
        label: "Attachments",
        component: {
          type: ComponentType.CheckboxGroup,
          custom_id: "bind_fullkit_attatchments",
          required: false,
          min_values: 0,
          max_values: 5,
          options: [
            { label: "Holosight", value: "holo" },
            { label: "Weapon Lasersight", value: "laser" },
            { label: "Extended Magazine", value: "extended_mag" },
            { label: "Weapon Flashlight", value: "flashlight" },
            { label: "Muzzle Boost", value: "muzzle_boost" },
          ],
        },
      },
      {
        type: ComponentType.Label,
        label: "Ammo",
        component: {
          type: ComponentType.RadioGroup,
          custom_id: "bind_fullkit_ammo",
          options: [
            { label: "5.56 Rifle Ammo", value: "5.56" },
            { label: "Incendiary 5.56 Rifle Ammo", value: "incen" },
            { label: "HV 5.56 Rifle Ammo", value: "hv" },
          ],
        },
      },
      {
        type: ComponentType.Label,
        label: "Amount of Ammo",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_fullkit_ammo_amount",
          style: TextInputStyle.Short,
          max_length: 3,
          placeholder: "Amount of ammo you want to be crafted",
          required: true,
        },
      },
      {
        type: ComponentType.Label,
        label: "Amount of barricades",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_fullkit_barricades_amount",
          style: TextInputStyle.Short,
          max_length: 2,
          placeholder: "Amount of barricades you want to be crafted",
          required: true,
        },
      },
    ]);
  });

  it("builds the second fullkit modal within Discord's 5 component limit", () => {
    const modal = __testables.buildBindFullkitFinishModal("draft-123").toJSON();

    expect(modal.title).toBe("Bind Fullkit");
    expect(modal.custom_id).toBe("bind_fullkit_modal_step_2:draft-123");
    expect(modal.components).toHaveLength(5);
    expect(modal.components).toEqual([
      {
        type: ComponentType.Label,
        label: "Amount of Medkits",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_fullkit_medkits_amount",
          style: TextInputStyle.Short,
          max_length: 2,
          placeholder: "Amount of medkits you want to be crafted",
          required: true,
        },
      },
      {
        type: ComponentType.Label,
        label: "Amount of Meds",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_fullkit_meds_amount",
          style: TextInputStyle.Short,
          max_length: 2,
          placeholder: "Amount of meds you want to be crafted",
          required: true,
        },
      },
      {
        type: ComponentType.Label,
        label: "Armor",
        component: {
          type: ComponentType.RadioGroup,
          custom_id: "bind_fullkit_armor",
          options: [
            { label: "Road Sign", value: "roadsign" },
            { label: "Metal", value: "metal" },
            { label: "Jacket", value: "jacket" },
          ],
        },
      },
      {
        type: ComponentType.Label,
        label: "Anything else?",
        component: {
          type: ComponentType.CheckboxGroup,
          custom_id: "bind_fullkit_anything_else",
          required: false,
          min_values: 0,
          max_values: 2,
          options: [
            { label: "Boots", value: "boots" },
            { label: "Shorts", value: "shorts" },
          ],
        },
      },
      {
        type: ComponentType.Label,
        label: "Bind",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_fullkit_bind",
          style: TextInputStyle.Short,
          max_length: 1,
          placeholder: "Specify a one key to make the bind for",
          required: true,
        },
      },
    ]);
  });

  it("builds the turret modal within Discord's 5 component limit", () => {
    const modal = __testables.buildBindTurretModal().toJSON();

    expect(modal.title).toBe("Bind Turret");
    expect(modal.custom_id).toBe("bind_turret_modal");
    expect(modal.components).toEqual([
      {
        type: ComponentType.Label,
        label: "Weapons",
        component: {
          type: ComponentType.RadioGroup,
          custom_id: "bind_turret_weapon",
          options: [
            { label: "Bolt Action Rifle", value: "bolt_action_rifle" },
            { label: "HMLMG", value: "hmlmg" },
            { label: "Python", value: "python" },
          ],
        },
      },
      {
        type: ComponentType.Label,
        label: "Ammo",
        component: {
          type: ComponentType.RadioGroup,
          custom_id: "bind_turret_ammo",
          options: [
            { label: "5.56 Rifle Ammo", value: "5.56" },
            { label: "Incendiary 5.56 Rifle Ammo", value: "incen" },
            { label: "HV 5.56 Rifle Ammo", value: "hv" },
          ],
        },
      },
      {
        type: ComponentType.Label,
        label: "Amount of Ammo",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_turret_ammo_amount",
          style: TextInputStyle.Short,
          max_length: 3,
          placeholder: "Amount of ammo you want to be crafted",
          required: true,
        },
      },
      {
        type: ComponentType.Label,
        label: "Anything else?",
        component: {
          type: ComponentType.CheckboxGroup,
          custom_id: "bind_turret_anything_else",
          required: false,
          min_values: 0,
          max_values: 2,
          options: [
            { label: "Muzzle Boost", value: "muzzle_boost" },
            { label: "Silencer", value: "silencer" },
          ],
        },
      },
      {
        type: ComponentType.Label,
        label: "Bind",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_turret_bind",
          style: TextInputStyle.Short,
          max_length: 1,
          placeholder: "Specify a one key to make the bind for",
          required: true,
        },
      },
    ]);
  });

  it("builds the rocket modal", () => {
    const modal = __testables.buildBindRocketModal().toJSON();

    expect(modal.title).toBe("Bind Rocket");
    expect(modal.custom_id).toBe("bind_rocket_modal");
    expect(modal.components).toEqual([
      {
        type: ComponentType.Label,
        label: "Amount of Rockets",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_rocket_amount",
          style: TextInputStyle.Short,
          max_length: 2,
          placeholder: "Amount of rockets you want to be crafted",
          required: true,
        },
      },
      {
        type: ComponentType.Label,
        label: "Anything else?",
        component: {
          type: ComponentType.CheckboxGroup,
          custom_id: "bind_rocket_anything_else",
          required: false,
          min_values: 0,
          max_values: 1,
          options: [{ label: "Pipes", value: "pipes" }],
        },
      },
      {
        type: ComponentType.Label,
        label: "Bind",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_rocket_bind",
          style: TextInputStyle.Short,
          max_length: 1,
          placeholder: "Specify a one key to make the bind for",
          required: true,
        },
      },
    ]);
  });

  it("builds the c4 modal", () => {
    const modal = __testables.buildBindC4Modal().toJSON();

    expect(modal.title).toBe("Bind C4");
    expect(modal.custom_id).toBe("bind_c4_modal");
    expect(modal.components).toEqual([
      {
        type: ComponentType.Label,
        label: "Amount of C4",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_c4_amount",
          style: TextInputStyle.Short,
          max_length: 2,
          placeholder: "Amount of c4 you want to be crafted",
          required: true,
        },
      },
      {
        type: ComponentType.Label,
        label: "Bind",
        component: {
          type: ComponentType.TextInput,
          custom_id: "bind_c4_bind",
          style: TextInputStyle.Short,
          max_length: 1,
          placeholder: "Specify a one key to make the bind for",
          required: true,
        },
      },
    ]);
  });

  it("extracts draft ids from prefixed custom ids", () => {
    expect(
      __testables.extractDraftId(
        "bind_fullkit_continue:abc123",
        "bind_fullkit_continue:",
      ),
    ).toBe("abc123");
    expect(
      __testables.extractDraftId(
        "bind_fullkit_modal_step_2:abc123",
        "bind_fullkit_modal_step_2:",
      ),
    ).toBe("abc123");
  });

  it("builds bind craft commands in the requested order", () => {
    const result = __testables.buildCraftCommands(
      {
        weapon: ["ak"],
        attachments: ["holo", "extended_mag"],
        ammo: ["5.56"],
        ammoAmount: ["7"],
      },
      3,
      2,
      4,
      ["roadsign"],
      ["boots"],
    );

    expect(result.error).toBeNull();
    expect(result.commands).toEqual([
      "craft.add 1545779598 1",
      "craft.add 1079279582 4",
      "craft.add 254522515 2",
      "craft.add 1079279582 3",
      "craft.add 1373240771 4",
      "craft.add 442289265 1",
      "craft.add 2005491391 1",
      "craft.add -1211166256 3",
      "craft.add -194953424 1",
      "craft.add 1850456855 1",
      "craft.add 1751045826 1",
      "craft.add 237239288 1",
      "craft.add -2002277461 1",
      "craft.add -1549739227 1",
    ]);
  });

  it("replaces the default pants item when shorts are selected", () => {
    const result = __testables.buildCraftCommands(
      {
        weapon: ["mp5"],
        attachments: [],
        ammo: ["incen"],
        ammoAmount: ["2"],
      },
      0,
      0,
      0,
      ["metal"],
      ["shorts"],
    );

    expect(result.error).toBeNull();
    expect(result.commands).toEqual([
      "craft.add 1318558775 1",
      "craft.add 605467368 1",
      "craft.add -194953424 1",
      "craft.add 1850456855 1",
      "craft.add 1751045826 1",
      "craft.add 1110385766 1",
      "craft.add -1695367501 1",
    ]);
  });

  it("splits meds and medkits across multiple craft commands when needed", () => {
    const result = __testables.buildCraftCommands(
      {
        weapon: ["mp5"],
        attachments: [],
        ammo: ["incen"],
        ammoAmount: ["2"],
      },
      13,
      12,
      9,
      ["metal"],
      ["shorts"],
    );

    expect(result.error).toBeNull();
    expect(result.commands).toEqual([
      "craft.add 1318558775 1",
      "craft.add 1079279582 10",
      "craft.add 1079279582 10",
      "craft.add 1079279582 4",
      "craft.add 254522515 5",
      "craft.add 254522515 5",
      "craft.add 254522515 2",
      "craft.add 1079279582 10",
      "craft.add 1079279582 3",
      "craft.add 1373240771 9",
      "craft.add 605467368 1",
      "craft.add -194953424 1",
      "craft.add 1850456855 1",
      "craft.add 1751045826 1",
      "craft.add 1110385766 1",
      "craft.add -1695367501 1",
    ]);
  });

  it("builds the final bind command and embed", () => {
    const command = __testables.buildBindCommand("f", [
      "craft.add 1 2",
      "craft.add 2 1",
    ]);
    const embed = __testables.buildFullkitBindEmbed(command).data;

    expect(command).toBe('bind f "craft.add 1 2;craft.add 2 1"');
    expect(embed.color).toBe(14582045);
    expect(embed.title).toBe("Fullkit bind");
    expect(embed.description).toBe('```\nbind f "craft.add 1 2;craft.add 2 1"\n```');
    expect(embed.footer).toEqual({
      text: "RustLens.com",
      icon_url:
        "https://i.ibb.co/n8MWVLDN/1ee153f3c14d7decaeb6cb62c28fb5b4.webp",
    });
  });

  it("builds turret craft commands in the requested order", () => {
    const result = __testables.buildTurretCraftCommands(
      ["hmlmg"],
      ["incen"],
      ["7"],
      ["muzzle_boost", "silencer"],
    );

    expect(result).toEqual({
      commands: [
        "craft.add -2139580305 1",
        "craft.add -1214542497 1",
        "craft.add 605467368 4",
        "craft.add -1405508498 1",
        "craft.add -781866273 1",
      ],
      error: null,
    });
  });

  it("builds the turret bind embed", () => {
    const embed = __testables.buildTurretBindEmbed('bind t "craft.add 1 1"').data;

    expect(embed.title).toBe("Turret bind");
    expect(embed.description).toBe('```\nbind t "craft.add 1 1"\n```');
  });

  it("builds rocket craft commands in the requested order and chunks rockets", () => {
    const result = __testables.buildRocketCraftCommands(["21"], ["pipes"]);

    expect(result).toEqual({
      commands: [
        "craft.add -265876753 1365",
        "craft.add -592016202 210",
        "craft.add 95950017 42",
        "craft.add -742865266 20",
        "craft.add -742865266 1",
      ],
      error: null,
    });
  });

  it("builds the rocket bind embed", () => {
    const embed = __testables.buildRocketBindEmbed('bind r "craft.add 1 1"').data;

    expect(embed.title).toBe("Rocket bind");
    expect(embed.description).toBe('```\nbind r "craft.add 1 1"\n```');
  });

  it("builds c4 craft commands in the requested order and chunks c4", () => {
    const result = __testables.buildC4CraftCommands(["21"]);

    expect(result).toEqual({
      commands: [
        "craft.add -265876753 2100",
        "craft.add -592016202 420",
        "craft.add 1248356124 20",
        "craft.add 1248356124 1",
      ],
      error: null,
    });
  });

  it("builds the c4 bind embed", () => {
    const embed = __testables.buildC4BindEmbed('bind c "craft.add 1 1"').data;

    expect(embed.title).toBe("C4 bind");
    expect(embed.description).toBe('```\nbind c "craft.add 1 1"\n```');
  });

  it("rejects missing numeric input and supports HV ammo", () => {
    expect(
      __testables.parseWholeNumber(["abc"], "Amount of Meds"),
    ).toEqual({
      error: "Amount of Meds must be a whole number.",
      value: null,
    });

    const hvResult = __testables.buildCraftCommands(
      {
        weapon: ["ak"],
        attachments: [],
        ammo: ["hv"],
        ammoAmount: ["3"],
      },
      0,
      0,
      0,
      ["jacket"],
      [],
    );

    expect(hvResult).toEqual({
      commands: [
        "craft.add 1545779598 1",
        "craft.add 1712070256 1",
        "craft.add -194953424 1",
        "craft.add 1850456855 1",
        "craft.add 1751045826 1",
        "craft.add 237239288 1",
        "craft.add -1163532624 1",
      ],
      error: null,
    });
  });
});
