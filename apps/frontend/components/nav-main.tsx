"use client"

import Link from "next/link"

import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { BotIcon } from "lucide-react"

export function NavMain({
  items,
  installBotUrl,
}: {
  items: {
    title: string
    url: string
    icon?: React.ReactNode
  }[]
  installBotUrl?: string
}) {
  return (
    <SidebarGroup>
      <SidebarGroupContent className="flex flex-col gap-2">
        {installBotUrl ? (
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton
                asChild
                tooltip="Install Bot"
                className="min-w-8 bg-primary text-primary-foreground duration-300 ease-[cubic-bezier(0.4,0,0.2,1)] hover:bg-primary/90 hover:text-primary-foreground active:bg-primary/90 active:text-primary-foreground"
              >
                <a
                  href={installBotUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <BotIcon />
                  <span>Install Bot</span>
                </a>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        ) : null}
        <SidebarMenu>
          {items.map((item) => (
            <SidebarMenuItem key={item.title}>
              <SidebarMenuButton asChild tooltip={item.title}>
                <Link href={item.url}>
                  {item.icon}
                  <span>{item.title}</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          ))}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}
