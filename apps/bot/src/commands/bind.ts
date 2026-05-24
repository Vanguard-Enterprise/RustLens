import { logger } from "../logger.js";
import { randomUUID } from "node:crypto";
import type {
  ButtonInteraction,
  ChatInputCommandInteraction,
  Client,
  ModalSubmitInteraction,
} from "discord.js";
import {
  ActionRowBuilder,
  ApplicationIntegrationType,
  ButtonBuilder,
  ButtonStyle,
  ChannelType,
  InteractionContextType,
  MessageFlags,
  PermissionFlagsBits,
  SlashCommandBuilder,
} from "discord.js";
import {
  CheckboxGroupBuilder,
  ModalV2Builder,
  RadioGroupBuilder,
  TextInputBuilder,
  TextInputStyle,
} from "../builders/index.js";
import { BackendClient } from "../backend-client.js";
import { loadConfig } from "../config.js";
import {
  createEmbed,
  getFieldValues,
  replyEphemeral,
} from "../shared.js";
import { getFollowupCorrelationId, getOrAssignCorrelationId, withCorrelation } from "../correlation.js";
import { hasGuildAdminAccess } from "../guild-settings.js";
import { getGuildMembers } from "../guild-member-cache.js";

export const BIND_COMMAND_NAME = "bind";

const BIND_MASSINVITE_SUBCOMMAND_NAME = "massinvite";
const BIND_FULLKIT_SUBCOMMAND_NAME = "fullkit";

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-bind`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}
const BIND_TURRET_SUBCOMMAND_NAME = "turret";
const BIND_ROCKET_SUBCOMMAND_NAME = "rocket";
const BIND_C4_SUBCOMMAND_NAME = "c4";
const BIND_FULLKIT_STEP_ONE_MODAL_CUSTOM_ID = "bind_fullkit_modal_step_1";
const BIND_FULLKIT_STEP_TWO_MODAL_PREFIX = "bind_fullkit_modal_step_2:";
const BIND_FULLKIT_CONTINUE_BUTTON_PREFIX = "bind_fullkit_continue:";
const BIND_FULLKIT_WEAPON_CUSTOM_ID = "bind_fullkit_weapon";
const BIND_FULLKIT_ATTACHMENTS_CUSTOM_ID = "bind_fullkit_attatchments";
const BIND_FULLKIT_AMMO_CUSTOM_ID = "bind_fullkit_ammo";
const BIND_FULLKIT_AMMO_AMOUNT_CUSTOM_ID = "bind_fullkit_ammo_amount";
const BIND_FULLKIT_MEDS_AMOUNT_CUSTOM_ID = "bind_fullkit_meds_amount";
const BIND_FULLKIT_MEDKITS_AMOUNT_CUSTOM_ID = "bind_fullkit_medkits_amount";
const BIND_FULLKIT_BARRICADES_AMOUNT_CUSTOM_ID = "bind_fullkit_barricades_amount";
const BIND_FULLKIT_ARMOR_CUSTOM_ID = "bind_fullkit_armor";
const BIND_FULLKIT_ANYTHING_ELSE_CUSTOM_ID = "bind_fullkit_anything_else";
const BIND_FULLKIT_BIND_CUSTOM_ID = "bind_fullkit_bind";
const BIND_TURRET_MODAL_CUSTOM_ID = "bind_turret_modal";
const BIND_TURRET_WEAPON_CUSTOM_ID = "bind_turret_weapon";
const BIND_TURRET_AMMO_CUSTOM_ID = "bind_turret_ammo";
const BIND_TURRET_AMMO_AMOUNT_CUSTOM_ID = "bind_turret_ammo_amount";
const BIND_TURRET_ANYTHING_ELSE_CUSTOM_ID = "bind_turret_anything_else";
const BIND_TURRET_BIND_CUSTOM_ID = "bind_turret_bind";
const BIND_ROCKET_MODAL_CUSTOM_ID = "bind_rocket_modal";
const BIND_ROCKET_AMOUNT_CUSTOM_ID = "bind_rocket_amount";
const BIND_ROCKET_ANYTHING_ELSE_CUSTOM_ID = "bind_rocket_anything_else";
const BIND_ROCKET_BIND_CUSTOM_ID = "bind_rocket_bind";
const BIND_C4_MODAL_CUSTOM_ID = "bind_c4_modal";
const BIND_C4_AMOUNT_CUSTOM_ID = "bind_c4_amount";
const BIND_C4_BIND_CUSTOM_ID = "bind_c4_bind";
const BIND_FULLKIT_DRAFT_TTL_MS = 15 * 60 * 1000;

type BindFullkitDraft = {
  userId: string;
  guildId: string | null;
  weapon: string[];
  attachments: string[];
  ammo: string[];
  ammoAmount: string[];
  barricadesAmount: string[];
  createdAt: number;
};

type CraftableOption = {
  value: string;
  label: string;
  itemIds: number[];
};

type AmmoOption = CraftableOption & {
  craftYield: number;
};

const bindFullkitDrafts = new Map<string, BindFullkitDraft>();

const weaponOptions: readonly CraftableOption[] = [
  { value: "ak", label: "Assault Rifle", itemIds: [1545779598] },
  { value: "mp5", label: "MP5A4", itemIds: [1318558775] },
  { value: "thompson", label: "Thompson", itemIds: [-1758372725] },
] as const;

const attachmentOptions: readonly CraftableOption[] = [
  { value: "holo", label: "Holosight", itemIds: [442289265] },
  { value: "laser", label: "Weapon Lasersight", itemIds: [-132516482] },
  { value: "extended_mag", label: "Extended Magazine", itemIds: [2005491391] },
  { value: "flashlight", label: "Weapon Flashlight", itemIds: [952603248] },
  { value: "muzzle_boost", label: "Muzzle Boost", itemIds: [-1405508498] },
] as const;

const ammoOptions: readonly AmmoOption[] = [
  { value: "5.56", label: "5.56 Rifle Ammo", itemIds: [-1211166256], craftYield: 3 },
  {
    value: "incen",
    label: "Incendiary 5.56 Rifle Ammo",
    itemIds: [605467368],
    craftYield: 2,
  },
  {
    value: "hv",
    label: "HV 5.56 Rifle Ammo",
    itemIds: [1712070256],
    craftYield: 3,
  },
] as const;

const armorOptions: readonly CraftableOption[] = [
  { value: "roadsign", label: "Road Sign", itemIds: [-2002277461] },
  { value: "metal", label: "Metal", itemIds: [1110385766] },
  { value: "jacket", label: "Jacket", itemIds: [-1163532624] },
] as const;

const extraArmorOptions: readonly CraftableOption[] = [
  { value: "boots", label: "Boots", itemIds: [-1549739227] },
  { value: "shorts", label: "Shorts", itemIds: [-1695367501] },
] as const;
const turretWeaponOptions: readonly CraftableOption[] = [
  {
    value: "bolt_action_rifle",
    label: "Bolt Action Rifle",
    itemIds: [1588298435],
  },
  { value: "hmlmg", label: "HMLMG", itemIds: [-1214542497] },
  { value: "python", label: "Python", itemIds: [1373971859] },
] as const;
const turretExtraOptions: readonly CraftableOption[] = [
  { value: "muzzle_boost", label: "Muzzle Boost", itemIds: [-1405508498] },
  { value: "silencer", label: "Silencer", itemIds: [-781866273] },
] as const;
const rocketExtraOptions: readonly CraftableOption[] = [
  { value: "pipes", label: "Pipes", itemIds: [95950017] },
] as const;

const medsItemId = 1079279582;
const medkitsItemId = 254522515;
const barricadesItemId = 1373240771;
const armorBaseItemIds = [-194953424, 1850456855, 1751045826, 237239288] as const;
const maxMedsPerCraftCommand = 10;
const maxMedkitsPerCraftCommand = 5;
const turretItemId = -2139580305;
const gunpowderItemId = -265876753;
const explosivesItemId = -592016202;
const rocketItemId = -742865266;
const c4ItemId = 1248356124;
const gunpowderCraftsPerRocket = 65;
const explosivesPerRocket = 10;
const pipesPerRocket = 2;
const maxRocketsPerCraftCommand = 20;
const maxC4PerCraftCommand = 20;
const gunpowderCraftsPerC4 = 100;
const explosivesPerC4 = 20;

const bindCommand = new SlashCommandBuilder()
  .setName(BIND_COMMAND_NAME)
  .setDescription("RustLens bind commands")
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(BIND_FULLKIT_SUBCOMMAND_NAME)
      .setDescription("Open the fullkit bind modal"),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(BIND_TURRET_SUBCOMMAND_NAME)
      .setDescription("Open the turret bind modal"),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(BIND_ROCKET_SUBCOMMAND_NAME)
      .setDescription("Open the rocket bind modal"),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(BIND_C4_SUBCOMMAND_NAME)
      .setDescription("Open the c4 bind modal"),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(BIND_MASSINVITE_SUBCOMMAND_NAME)
      .setDescription("Generate a clan invite bind for all linked Steam IDs in a role")
      .addStringOption((option) =>
        option
          .setName("key")
          .setDescription("The key to bind to")
          .setRequired(true)
          .setMaxLength(1),
      )
      .addRoleOption((option) =>
        option
          .setName("scope")
          .setDescription("The role whose members to invite")
          .setRequired(true),
      )
      .addChannelOption((option) =>
        option
          .setName("vc")
          .setDescription("Only include members in this voice channel")
          .addChannelTypes(ChannelType.GuildVoice, ChannelType.GuildStageVoice)
          .setRequired(false),
      ),
  );

function buildBindFullkitStartModal(correlation: string) {
  return new ModalV2Builder()
    .setTitle("Bind Fullkit")
    .setCustomId(withCorrelation(BIND_FULLKIT_STEP_ONE_MODAL_CUSTOM_ID, correlation))
    .addComponent(
      "Weapon",
      new RadioGroupBuilder()
        .setCustomId(BIND_FULLKIT_WEAPON_CUSTOM_ID)
        .addOptions(...weaponOptions.map(({ label, value }) => ({ label, value }))),
    )
    .addComponent(
      "Attachments",
      new CheckboxGroupBuilder()
        .setCustomId(BIND_FULLKIT_ATTACHMENTS_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(5)
        .addOptions(
          ...attachmentOptions.map(({ label, value }) => ({ label, value })),
        ),
    )
    .addComponent(
      "Ammo",
      new RadioGroupBuilder()
        .setCustomId(BIND_FULLKIT_AMMO_CUSTOM_ID)
        .addOptions(...ammoOptions.map(({ label, value }) => ({ label, value }))),
    )
    .addComponent(
      "Amount of Ammo",
      new TextInputBuilder()
        .setCustomId(BIND_FULLKIT_AMMO_AMOUNT_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(3)
        .setPlaceholder("Amount of ammo you want to be crafted"),
    )
    .addComponent(
      "Amount of barricades",
      new TextInputBuilder()
        .setCustomId(BIND_FULLKIT_BARRICADES_AMOUNT_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(2)
        .setPlaceholder("Amount of barricades you want to be crafted"),
    );
}

function buildBindFullkitFinishModal(draftId: string, correlation: string) {
  return new ModalV2Builder()
    .setTitle("Bind Fullkit")
    .setCustomId(withCorrelation(`${BIND_FULLKIT_STEP_TWO_MODAL_PREFIX}${draftId}`, correlation))
    .addComponent(
      "Amount of Medkits",
      new TextInputBuilder()
        .setCustomId(BIND_FULLKIT_MEDKITS_AMOUNT_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(2)
        .setPlaceholder("Amount of medkits you want to be crafted"),
    )
    .addComponent(
      "Amount of Meds",
      new TextInputBuilder()
        .setCustomId(BIND_FULLKIT_MEDS_AMOUNT_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(2)
        .setPlaceholder("Amount of meds you want to be crafted"),
    )
    .addComponent(
      "Armor",
      new RadioGroupBuilder()
        .setCustomId(BIND_FULLKIT_ARMOR_CUSTOM_ID)
        .addOptions(...armorOptions.map(({ label, value }) => ({ label, value }))),
    )
    .addComponent(
      "Anything else?",
      new CheckboxGroupBuilder()
        .setCustomId(BIND_FULLKIT_ANYTHING_ELSE_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(2)
        .addOptions(
          ...extraArmorOptions.map(({ label, value }) => ({ label, value })),
        ),
    )
    .addComponent(
      "Bind",
      new TextInputBuilder()
        .setCustomId(BIND_FULLKIT_BIND_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(1)
        .setPlaceholder("Specify a one key to make the bind for"),
    )
    ;
}

function buildBindTurretModal(correlation: string) {
  return new ModalV2Builder()
    .setTitle("Bind Turret")
    .setCustomId(withCorrelation(BIND_TURRET_MODAL_CUSTOM_ID, correlation))
    .addComponent(
      "Weapons",
      new RadioGroupBuilder()
        .setCustomId(BIND_TURRET_WEAPON_CUSTOM_ID)
        .addOptions(
          ...turretWeaponOptions.map(({ label, value }) => ({ label, value })),
        ),
    )
    .addComponent(
      "Ammo",
      new RadioGroupBuilder()
        .setCustomId(BIND_TURRET_AMMO_CUSTOM_ID)
        .addOptions(...ammoOptions.map(({ label, value }) => ({ label, value }))),
    )
    .addComponent(
      "Amount of Ammo",
      new TextInputBuilder()
        .setCustomId(BIND_TURRET_AMMO_AMOUNT_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(3)
        .setPlaceholder("Amount of ammo you want to be crafted"),
    )
    .addComponent(
      "Anything else?",
      new CheckboxGroupBuilder()
        .setCustomId(BIND_TURRET_ANYTHING_ELSE_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(2)
        .addOptions(
          ...turretExtraOptions.map(({ label, value }) => ({ label, value })),
        ),
    )
    .addComponent(
      "Bind",
      new TextInputBuilder()
        .setCustomId(BIND_TURRET_BIND_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(1)
        .setPlaceholder("Specify a one key to make the bind for"),
    );
}

function buildBindRocketModal(correlation: string) {
  return new ModalV2Builder()
    .setTitle("Bind Rocket")
    .setCustomId(withCorrelation(BIND_ROCKET_MODAL_CUSTOM_ID, correlation))
    .addComponent(
      "Amount of Rockets",
      new TextInputBuilder()
        .setCustomId(BIND_ROCKET_AMOUNT_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(2)
        .setPlaceholder("Amount of rockets you want to be crafted"),
    )
    .addComponent(
      "Anything else?",
      new CheckboxGroupBuilder()
        .setCustomId(BIND_ROCKET_ANYTHING_ELSE_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(1)
        .addOptions(
          ...rocketExtraOptions.map(({ label, value }) => ({ label, value })),
        ),
    )
    .addComponent(
      "Bind",
      new TextInputBuilder()
        .setCustomId(BIND_ROCKET_BIND_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(1)
        .setPlaceholder("Specify a one key to make the bind for"),
    );
}

function buildBindC4Modal(correlation: string) {
  return new ModalV2Builder()
    .setTitle("Bind C4")
    .setCustomId(withCorrelation(BIND_C4_MODAL_CUSTOM_ID, correlation))
    .addComponent(
      "Amount of C4",
      new TextInputBuilder()
        .setCustomId(BIND_C4_AMOUNT_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(2)
        .setPlaceholder("Amount of c4 you want to be crafted"),
    )
    .addComponent(
      "Bind",
      new TextInputBuilder()
        .setCustomId(BIND_C4_BIND_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(1)
        .setPlaceholder("Specify a one key to make the bind for"),
    );
}

function createBindFullkitContinueRow(draftId: string, correlation: string) {
  return new ActionRowBuilder<ButtonBuilder>().addComponents(
    new ButtonBuilder()
      .setCustomId(withCorrelation(`${BIND_FULLKIT_CONTINUE_BUTTON_PREFIX}${draftId}`, correlation))
      .setLabel("Step 2")
      .setEmoji("⚙️")
      .setStyle(ButtonStyle.Success),
  );
}

function pruneExpiredBindFullkitDrafts(now = Date.now()) {
  for (const [draftId, draft] of bindFullkitDrafts) {
    if (now - draft.createdAt > BIND_FULLKIT_DRAFT_TTL_MS) {
      bindFullkitDrafts.delete(draftId);
    }
  }
}

function extractDraftId(customId: string, prefix: string) {
  if (!customId.startsWith(prefix)) {
    return null;
  }

  const draftId = customId.slice(prefix.length);
  return draftId.length > 0 ? draftId : null;
}


function parseWholeNumber(values: string[], label: string) {
  const value = values[0]?.trim() ?? "";
  if (!/^\d+$/.test(value)) {
    return {
      error: `${label} must be a whole number.`,
      value: null,
    } as const;
  }

  return {
    error: null,
    value: Number(value),
  } as const;
}

function findOption<T extends { value: string }>(
  options: readonly T[],
  selectedValue: string | undefined,
) {
  if (!selectedValue) {
    return null;
  }

  return options.find((option) => option.value === selectedValue) ?? null;
}

function buildCraftCommands(
  draft: Pick<BindFullkitDraft, "weapon" | "attachments" | "ammo" | "ammoAmount">,
  medsAmount: number,
  medkitsAmount: number,
  barricadesAmount: number,
  armor: string[],
  anythingElse: string[],
) {
  const commands: string[] = [];
  const addCraft = (itemId: number, amount = 1) => {
    if (amount <= 0) {
      return;
    }
    commands.push(`craft.add ${itemId} ${amount}`);
  };
  const addChunkedCraft = (itemId: number, totalAmount: number, chunkSize: number) => {
    let remaining = totalAmount;
    while (remaining > 0) {
      const amount = Math.min(remaining, chunkSize);
      addCraft(itemId, amount);
      remaining -= amount;
    }
  };

  const weapon = findOption(weaponOptions, draft.weapon[0]);
  if (!weapon) {
    return {
      commands: [],
      error: "Select a valid weapon.",
    } as const;
  }
  for (const itemId of weapon.itemIds) {
    addCraft(itemId);
  }

  const medsForMedkits = medkitsAmount * 2;
  addChunkedCraft(medsItemId, medsForMedkits, maxMedsPerCraftCommand);
  addChunkedCraft(medkitsItemId, medkitsAmount, maxMedkitsPerCraftCommand);
  addChunkedCraft(medsItemId, medsAmount, maxMedsPerCraftCommand);
  addCraft(barricadesItemId, barricadesAmount);

  const selectedAttachments = new Set(draft.attachments);
  for (const option of attachmentOptions) {
    if (selectedAttachments.has(option.value)) {
      for (const itemId of option.itemIds) {
        addCraft(itemId);
      }
    }
  }

  const ammo = findOption(ammoOptions, draft.ammo[0]);
  if (!ammo) {
    return {
      commands: [],
      error: "Select a valid ammo type.",
    } as const;
  }
  if (ammo.itemIds.length === 0) {
    return {
      commands: [],
      error: `${ammo.label} is not configured yet because its item id is missing.`,
    } as const;
  }

  const ammoAmountResult = parseWholeNumber(draft.ammoAmount, "Amount of Ammo");
  if (ammoAmountResult.error || ammoAmountResult.value === null) {
    return {
      commands: [],
      error: ammoAmountResult.error,
    } as const;
  }

  const ammoCraftCount = Math.ceil(ammoAmountResult.value / ammo.craftYield);
  for (const itemId of ammo.itemIds) {
    addCraft(itemId, ammoCraftCount);
  }

  const armorOption = findOption(armorOptions, armor[0]);
  if (!armorOption) {
    return {
      commands: [],
      error: "Select a valid armor option.",
    } as const;
  }

  const selectedExtras = new Set(anythingElse);
  const includePants = !selectedExtras.has("shorts");
  for (const itemId of armorBaseItemIds) {
    if (itemId === 237239288 && !includePants) {
      continue;
    }
    addCraft(itemId);
  }
  for (const itemId of armorOption.itemIds) {
    addCraft(itemId);
  }
  for (const option of extraArmorOptions) {
    if (selectedExtras.has(option.value)) {
      for (const itemId of option.itemIds) {
        addCraft(itemId);
      }
    }
  }

  return {
    commands,
    error: null,
  } as const;
}

function buildTurretCraftCommands(
  weaponValues: string[],
  ammoValues: string[],
  ammoAmountValues: string[],
  anythingElseValues: string[],
) {
  const commands: string[] = [];
  const addCraft = (itemId: number, amount = 1) => {
    if (amount <= 0) {
      return;
    }
    commands.push(`craft.add ${itemId} ${amount}`);
  };

  addCraft(turretItemId);

  const weapon = findOption(turretWeaponOptions, weaponValues[0]);
  if (!weapon) {
    return {
      commands: [],
      error: "Select a valid turret weapon.",
    } as const;
  }
  for (const itemId of weapon.itemIds) {
    addCraft(itemId);
  }

  const ammo = findOption(ammoOptions, ammoValues[0]);
  if (!ammo) {
    return {
      commands: [],
      error: "Select a valid ammo type.",
    } as const;
  }

  const ammoAmountResult = parseWholeNumber(ammoAmountValues, "Amount of Ammo");
  if (ammoAmountResult.error || ammoAmountResult.value === null) {
    return {
      commands: [],
      error: ammoAmountResult.error,
    } as const;
  }

  const ammoCraftCount = Math.ceil(ammoAmountResult.value / ammo.craftYield);
  for (const itemId of ammo.itemIds) {
    addCraft(itemId, ammoCraftCount);
  }

  const selectedExtras = new Set(anythingElseValues);
  for (const option of turretExtraOptions) {
    if (selectedExtras.has(option.value)) {
      for (const itemId of option.itemIds) {
        addCraft(itemId);
      }
    }
  }

  return {
    commands,
    error: null,
  } as const;
}

function addChunkedCraftCommands(
  commands: string[],
  itemId: number,
  totalAmount: number,
  chunkSize: number,
) {
  if (totalAmount <= 0) {
    return;
  }

  let remaining = totalAmount;
  while (remaining > 0) {
    const amount = Math.min(remaining, chunkSize);
    commands.push(`craft.add ${itemId} ${amount}`);
    remaining -= amount;
  }
}

function buildRocketCraftCommands(amountValues: string[], anythingElseValues: string[]) {
  const rocketAmountResult = parseWholeNumber(amountValues, "Amount of Rockets");
  if (rocketAmountResult.error || rocketAmountResult.value === null) {
    return {
      commands: [],
      error: rocketAmountResult.error,
    } as const;
  }

  const rocketAmount = rocketAmountResult.value;
  const commands: string[] = [
    `craft.add ${gunpowderItemId} ${rocketAmount * gunpowderCraftsPerRocket}`,
    `craft.add ${explosivesItemId} ${rocketAmount * explosivesPerRocket}`,
  ];

  const selectedExtras = new Set(anythingElseValues);
  if (selectedExtras.has("pipes")) {
    commands.push(`craft.add 95950017 ${rocketAmount * pipesPerRocket}`);
  }

  addChunkedCraftCommands(commands, rocketItemId, rocketAmount, maxRocketsPerCraftCommand);

  return {
    commands,
    error: null,
  } as const;
}

function buildC4CraftCommands(amountValues: string[]) {
  const c4AmountResult = parseWholeNumber(amountValues, "Amount of C4");
  if (c4AmountResult.error || c4AmountResult.value === null) {
    return {
      commands: [],
      error: c4AmountResult.error,
    } as const;
  }

  const c4Amount = c4AmountResult.value;
  const commands = [
    `craft.add ${gunpowderItemId} ${c4Amount * gunpowderCraftsPerC4}`,
    `craft.add ${explosivesItemId} ${c4Amount * explosivesPerC4}`,
  ];
  addChunkedCraftCommands(commands, c4ItemId, c4Amount, maxC4PerCraftCommand);

  return {
    commands,
    error: null,
  } as const;
}

function buildBindCommand(key: string, commands: readonly string[]) {
  return `bind ${key} "${commands.join(";")}"`;
}

function buildFullkitBindEmbed(command: string) {
  return buildBindEmbed("Fullkit bind", command);
}

function buildTurretBindEmbed(command: string) {
  return buildBindEmbed("Turret bind", command);
}

function buildRocketBindEmbed(command: string) {
  return buildBindEmbed("Rocket bind", command);
}

function buildC4BindEmbed(command: string) {
  return buildBindEmbed("C4 bind", command);
}

function buildBindEmbed(title: string, command: string) {
  return createEmbed()
    .setTitle(title)
    .setDescription(`\`\`\`\n${command}\n\`\`\``);
}


async function handleMassInvite(interaction: ChatInputCommandInteraction) {
  const guild = interaction.guild;
  if (!guild) {
    await replyEphemeral(interaction, "Could not resolve guild.");
    return;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  if (!(await hasGuildAdminAccess(interaction))) {
    await replyEphemeral(interaction, "Only the configured admin role or administrators can use this command.");
    return;
  }

  const key = interaction.options.getString("key", true).trim();
  const role = interaction.options.getRole("scope", true);
  const vcChannel = interaction.options.getChannel("vc", false);

  const allMembers = await getGuildMembers(guild);
  let targetMembers = allMembers.filter((member) => member.roles.cache.has(role.id));

  if (vcChannel) {
    targetMembers = targetMembers.filter(
      (member) => member.voice.channelId === vcChannel.id,
    );
  }

  if (targetMembers.size === 0) {
    await interaction.editReply({
      content: vcChannel
        ? `No members found with <@&${role.id}> in <#${vcChannel.id}>.`
        : `No members found with <@&${role.id}>.`,
    });
    return;
  }

  const backend = getBackend();
  const workerId = getWorkerID();
  const { items: guildLinks } = await backend.listGuildLinks(workerId, guild.id);

  const discordToSteam = new Map<string, string>();
  for (const link of guildLinks) {
    discordToSteam.set(link.discordId, link.steamId);
  }

  const steamIds: string[] = [];
  for (const [, member] of targetMembers) {
    const steamId = discordToSteam.get(member.id);
    if (steamId) {
      steamIds.push(steamId);
    }
  }

  if (steamIds.length === 0) {
    await interaction.editReply({
      content: vcChannel
        ? `No linked Steam IDs found for members with <@&${role.id}> in <#${vcChannel.id}>.`
        : `No linked Steam IDs found for members with <@&${role.id}>.`,
    });
    return;
  }

  const chatCommands = steamIds.map((id) => `/clan invite ${id}`);
  const bindStr = `bind ${key} chat.say "${chatCommands.join(";")}"`;

  await interaction.editReply({
    embeds: [buildBindEmbed("Mass invite bind", bindStr)],
  });
}

export async function ensureBindCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = bindCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  logger.info("ensured /bind command (global)");
}

export async function handleBindChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== BIND_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyEphemeral(interaction, "Bind can only be used inside a server.");
    return true;
  }

  const subcommand = interaction.options.getSubcommand(false);
  const correlation = getOrAssignCorrelationId(interaction);
  switch (subcommand) {
    case BIND_FULLKIT_SUBCOMMAND_NAME:
      await interaction.showModal(buildBindFullkitStartModal(correlation).toJSON() as any);
      return true;
    case BIND_TURRET_SUBCOMMAND_NAME:
      await interaction.showModal(buildBindTurretModal(correlation).toJSON() as any);
      return true;
    case BIND_ROCKET_SUBCOMMAND_NAME:
      await interaction.showModal(buildBindRocketModal(correlation).toJSON() as any);
      return true;
    case BIND_C4_SUBCOMMAND_NAME:
      await interaction.showModal(buildBindC4Modal(correlation).toJSON() as any);
      return true;
    case BIND_MASSINVITE_SUBCOMMAND_NAME:
      await handleMassInvite(interaction);
      return true;
    default:
      await replyEphemeral(
        interaction,
        `Unknown /${BIND_COMMAND_NAME} subcommand: ${subcommand}`,
      );
      return true;
  }
}

export async function handleBindButtonInteraction(interaction: ButtonInteraction) {
  const draftId = extractDraftId(interaction.customId, BIND_FULLKIT_CONTINUE_BUTTON_PREFIX);
  if (!draftId) {
    return false;
  }

  pruneExpiredBindFullkitDrafts();

  const draft = bindFullkitDrafts.get(draftId);
  if (!draft || draft.userId !== interaction.user.id || draft.guildId !== interaction.guildId) {
    await replyEphemeral(interaction, "That fullkit bind draft expired. Run /bind fullkit again.");
    return true;
  }

  // Propagate the original /bind fullkit correlation so the step-2 modal
  // submit folds under the same slash-command row instead of becoming a child
  // of this button click.
  await interaction.showModal(
    buildBindFullkitFinishModal(draftId, getFollowupCorrelationId(interaction)).toJSON() as any,
  );
  return true;
}

export async function handleBindModalSubmit(interaction: ModalSubmitInteraction) {
  if (interaction.customId === BIND_ROCKET_MODAL_CUSTOM_ID) {
    const bindKey = getFieldValues(interaction, BIND_ROCKET_BIND_CUSTOM_ID)[0]?.trim() ?? "";
    if (bindKey.length === 0) {
      await replyEphemeral(interaction, "Bind is required.");
      return true;
    }

    const craftCommands = buildRocketCraftCommands(
      getFieldValues(interaction, BIND_ROCKET_AMOUNT_CUSTOM_ID),
      getFieldValues(interaction, BIND_ROCKET_ANYTHING_ELSE_CUSTOM_ID),
    );
    if (craftCommands.error) {
      await replyEphemeral(interaction, craftCommands.error);
      return true;
    }

    const bindCommand = buildBindCommand(bindKey, craftCommands.commands);
    await interaction.reply({
      embeds: [buildRocketBindEmbed(bindCommand)],
    });
    return true;
  }

  if (interaction.customId === BIND_C4_MODAL_CUSTOM_ID) {
    const bindKey = getFieldValues(interaction, BIND_C4_BIND_CUSTOM_ID)[0]?.trim() ?? "";
    if (bindKey.length === 0) {
      await replyEphemeral(interaction, "Bind is required.");
      return true;
    }

    const craftCommands = buildC4CraftCommands(
      getFieldValues(interaction, BIND_C4_AMOUNT_CUSTOM_ID),
    );
    if (craftCommands.error) {
      await replyEphemeral(interaction, craftCommands.error);
      return true;
    }

    const bindCommand = buildBindCommand(bindKey, craftCommands.commands);
    await interaction.reply({
      embeds: [buildC4BindEmbed(bindCommand)],
    });
    return true;
  }

  if (interaction.customId === BIND_TURRET_MODAL_CUSTOM_ID) {
    const bindKey = getFieldValues(interaction, BIND_TURRET_BIND_CUSTOM_ID)[0]?.trim() ?? "";
    if (bindKey.length === 0) {
      await replyEphemeral(interaction, "Bind is required.");
      return true;
    }

    const craftCommands = buildTurretCraftCommands(
      getFieldValues(interaction, BIND_TURRET_WEAPON_CUSTOM_ID),
      getFieldValues(interaction, BIND_TURRET_AMMO_CUSTOM_ID),
      getFieldValues(interaction, BIND_TURRET_AMMO_AMOUNT_CUSTOM_ID),
      getFieldValues(interaction, BIND_TURRET_ANYTHING_ELSE_CUSTOM_ID),
    );
    if (craftCommands.error) {
      await replyEphemeral(interaction, craftCommands.error);
      return true;
    }

    const bindCommand = buildBindCommand(bindKey, craftCommands.commands);
    await interaction.reply({
      embeds: [buildTurretBindEmbed(bindCommand)],
    });
    return true;
  }

  if (interaction.customId === BIND_FULLKIT_STEP_ONE_MODAL_CUSTOM_ID) {
    pruneExpiredBindFullkitDrafts();

    const draftId = randomUUID();
    bindFullkitDrafts.set(draftId, {
      userId: interaction.user.id,
      guildId: interaction.guildId,
      weapon: getFieldValues(interaction, BIND_FULLKIT_WEAPON_CUSTOM_ID),
      attachments: getFieldValues(interaction, BIND_FULLKIT_ATTACHMENTS_CUSTOM_ID),
      ammo: getFieldValues(interaction, BIND_FULLKIT_AMMO_CUSTOM_ID),
      ammoAmount: getFieldValues(interaction, BIND_FULLKIT_AMMO_AMOUNT_CUSTOM_ID),
      barricadesAmount: getFieldValues(interaction, BIND_FULLKIT_BARRICADES_AMOUNT_CUSTOM_ID),
      createdAt: Date.now(),
    });

    await replyEphemeral(
      interaction,
      "Step 1 saved. Please continue to step to via the button below.",
      [createBindFullkitContinueRow(draftId, getFollowupCorrelationId(interaction))],
    );
    return true;
  }

  const draftId = extractDraftId(interaction.customId, BIND_FULLKIT_STEP_TWO_MODAL_PREFIX);
  if (!draftId) {
    return false;
  }

  pruneExpiredBindFullkitDrafts();

  const draft = bindFullkitDrafts.get(draftId);
  if (!draft || draft.userId !== interaction.user.id || draft.guildId !== interaction.guildId) {
    await replyEphemeral(interaction, "That fullkit bind draft expired. Run /bind fullkit again.");
    return true;
  }

  bindFullkitDrafts.delete(draftId);

  const medsAmountResult = parseWholeNumber(
    getFieldValues(interaction, BIND_FULLKIT_MEDS_AMOUNT_CUSTOM_ID),
    "Amount of Meds",
  );
  if (medsAmountResult.error || medsAmountResult.value === null) {
    await replyEphemeral(interaction, medsAmountResult.error ?? "Invalid meds amount.");
    return true;
  }

  const medkitsAmountResult = parseWholeNumber(
    getFieldValues(interaction, BIND_FULLKIT_MEDKITS_AMOUNT_CUSTOM_ID),
    "Amount of Medkits",
  );
  if (medkitsAmountResult.error || medkitsAmountResult.value === null) {
    await replyEphemeral(interaction, medkitsAmountResult.error ?? "Invalid medkits amount.");
    return true;
  }

  const barricadesAmountResult = parseWholeNumber(
    draft.barricadesAmount,
    "Amount of barricades",
  );
  if (barricadesAmountResult.error || barricadesAmountResult.value === null) {
    await replyEphemeral(interaction, barricadesAmountResult.error ?? "Invalid barricades amount.");
    return true;
  }

  const armorValues = getFieldValues(interaction, BIND_FULLKIT_ARMOR_CUSTOM_ID);
  const anythingElseValues = getFieldValues(
    interaction,
    BIND_FULLKIT_ANYTHING_ELSE_CUSTOM_ID,
  );
  const bindKey = getFieldValues(interaction, BIND_FULLKIT_BIND_CUSTOM_ID)[0]?.trim() ?? "";
  if (bindKey.length === 0) {
    await replyEphemeral(interaction, "Bind is required.");
    return true;
  }

  const craftCommands = buildCraftCommands(
    draft,
    medsAmountResult.value,
    medkitsAmountResult.value,
    barricadesAmountResult.value,
    armorValues,
    anythingElseValues,
  );
  if (craftCommands.error) {
    await replyEphemeral(interaction, craftCommands.error);
    return true;
  }

  const bindCommand = buildBindCommand(bindKey, craftCommands.commands);

  await interaction.reply({
    embeds: [buildFullkitBindEmbed(bindCommand)],
  });

  return true;
}

export const __testables = {
  buildBindCommand,
  buildBindC4Modal,
  buildCraftCommands,
  buildBindFullkitFinishModal,
  buildBindFullkitStartModal,
  buildBindRocketModal,
  buildBindTurretModal,
  buildC4BindEmbed,
  buildC4CraftCommands,
  buildFullkitBindEmbed,
  buildRocketBindEmbed,
  buildRocketCraftCommands,
  buildTurretBindEmbed,
  buildTurretCraftCommands,
  extractDraftId,
  parseWholeNumber,
};
